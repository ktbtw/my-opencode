package app

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"launcher/internal/aiconfig"
	"launcher/internal/config"
	"launcher/internal/model"
)

// 构造一个可控制响应延迟的 /config/providers 测试服务。
func providerTestServer(t *testing.T, delay time.Duration, hits *int32, mu *sync.Mutex) int {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != providerMetadataPath {
			http.NotFound(w, r)
			return
		}
		if mu != nil && hits != nil {
			mu.Lock()
			*hits++
			mu.Unlock()
		}
		if delay > 0 {
			time.Sleep(delay)
		}
		_, _ = w.Write([]byte(`{"providers":[
		  {"id":"relay","models":{"gpt-5.4":{"id":"gpt-5.4","providerID":"relay","limit":{"context":4096,"output":1024},"capabilities":{"input":{"text":true},"output":{"text":true}}}}}
		]}`))
	}))
	t.Cleanup(server.Close)
	port, err := strconv.Atoi(strings.TrimPrefix(server.URL, "http://127.0.0.1:"))
	if err != nil {
		t.Fatal(err)
	}
	return port
}

// waitForProviderMetadata 等待后台刷新把元数据写入缓存。
func waitForProviderMetadata(t *testing.T, svc *service, key string) aiconfig.RuntimeProviderMetadata {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cached, _ := svc.cachedProviderMetadata(key); cached != nil && len(cached.All) > 0 {
			return *cached
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待后台刷新超时，元数据始终为空")
	return aiconfig.RuntimeProviderMetadata{}
}

// primeProviderMetadata 同步抓取一次并写入缓存。
//
// 运行时代理元数据现在是后台补齐的（不阻塞请求），断言 enrich 结果的测试
// 需要先把它准备好，否则读到的是首次的空快照。
func primeProviderMetadata(t *testing.T, svc *service, ports []int) {
	t.Helper()
	key := providerPortsKey(ports)
	metadata := svc.fetchProviderMetadata(ports)
	if len(metadata.All) == 0 {
		t.Fatalf("测试前置条件失败：未抓到任何供应商元数据")
	}
	svc.storeProviderMetadata(key, metadata)
}

// 首次调用必须同步拿到数据：/config/providers 约 0.6 秒，远低于后端 12 秒预算，
// 因此不该像 /provider 那样退化成空快照等后台补齐。
func TestRuntimeProviderMetadataReturnsDataOnFirstCall(t *testing.T) {
	port := providerTestServer(t, 0, nil, nil)
	svc := &service{
		cfg:    config.Config{RuntimeDir: t.TempDir()},
		agents: []model.Agent{{AgentID: "a", Status: "running", Port: port, Enabled: true}},
	}

	metadata, err := svc.runtimeProviderMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata.All) == 0 {
		t.Fatalf("首次调用应同步返回数据，实际为空: %+v", metadata)
	}
	if len(metadata.Connected) == 0 {
		t.Fatalf("首次调用应返回 connected 列表: %+v", metadata)
	}
}

// 新接口只返回已配置的供应商，这些 ID 必须全部计入 connected，
// 否则 EnrichWithRuntimeProviderMetadata 会把它们过滤掉。
func TestRuntimeProviderMetadataTreatsConfiguredProvidersAsConnected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != providerMetadataPath {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"providers":[
		  {"id":"自己用","models":{"m1":{"id":"m1","limit":{"context":100,"output":10}}}},
		  {"id":"订阅ds","models":{"m2":{"id":"m2","limit":{"context":200,"output":20}}}}
		]}`))
	}))
	defer server.Close()
	port, err := strconv.Atoi(strings.TrimPrefix(server.URL, "http://127.0.0.1:"))
	if err != nil {
		t.Fatal(err)
	}

	svc := &service{
		cfg:    config.Config{RuntimeDir: t.TempDir()},
		agents: []model.Agent{{AgentID: "a", Status: "running", Port: port, Enabled: true}},
	}
	metadata, err := svc.runtimeProviderMetadata()
	if err != nil {
		t.Fatal(err)
	}
	connected := map[string]bool{}
	for _, id := range metadata.Connected {
		connected[id] = true
	}
	for _, want := range []string{"自己用", "订阅ds"} {
		if !connected[want] {
			t.Fatalf("已配置供应商 %q 未计入 connected: %+v", want, metadata.Connected)
		}
	}
	if len(metadata.All) != 2 {
		t.Fatalf("应返回两个供应商，实际 %d: %+v", len(metadata.All), metadata.All)
	}
	// 每个返回的供应商都必须能被 connected 命中，否则会被 enrich 丢弃。
	for _, provider := range metadata.All {
		if !connected[provider.ID] {
			t.Fatalf("供应商 %q 在 All 中但不在 Connected 中，会被 enrich 过滤", provider.ID)
		}
	}
}

// 多个 agent 必须并发抓取：串行会让每个 agent 各等一次延迟。
func TestRuntimeProviderMetadataFetchesAgentsConcurrently(t *testing.T) {
	const delay = 600 * time.Millisecond
	var hits int32
	var mu sync.Mutex
	portA := providerTestServer(t, delay, &hits, &mu)
	portB := providerTestServer(t, delay, &hits, &mu)
	portC := providerTestServer(t, delay, &hits, &mu)

	svc := &service{
		cfg: config.Config{RuntimeDir: t.TempDir()},
		agents: []model.Agent{
			{AgentID: "a", Status: "running", Port: portA, Enabled: true},
			{AgentID: "b", Status: "running", Port: portB, Enabled: true},
			{AgentID: "c", Status: "running", Port: portC, Enabled: true},
		},
	}

	start := time.Now()
	if _, err := svc.runtimeProviderMetadata(); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	// 并发：总耗时接近单次延迟；串行会是 3 倍。
	if elapsed >= delay*2 {
		t.Fatalf("三个 agent 未并发抓取，耗时 %v（单次延迟 %v）", elapsed, delay)
	}
	mu.Lock()
	total := hits
	mu.Unlock()
	if total != 3 {
		t.Fatalf("期望每个 agent 各抓一次，实际 %d 次", total)
	}
}

// 单个 agent 抓取失败或超时不应影响其他 agent 的数据。
func TestRuntimeProviderMetadataSkipsSlowAgent(t *testing.T) {
	original := providerMetadataFetchTimeout
	providerMetadataFetchTimeout = 400 * time.Millisecond
	t.Cleanup(func() { providerMetadataFetchTimeout = original })

	// 延迟远超抓取超时，模拟返回数 MB 全量目录的慢 agent。
	slow := providerTestServer(t, 3*time.Second, nil, nil)
	fast := providerTestServer(t, 0, nil, nil)

	svc := &service{
		cfg: config.Config{RuntimeDir: t.TempDir()},
		agents: []model.Agent{
			{AgentID: "slow", Status: "running", Port: slow, Enabled: true},
			{AgentID: "fast", Status: "running", Port: fast, Enabled: true},
		},
	}

	// 首次调用同步抓取：慢 agent 被超时跳过，快 agent 的数据应当立即返回。
	got, err := svc.runtimeProviderMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.All) == 0 {
		t.Fatalf("快 agent 的数据被慢 agent 拖累而丢失: %+v", got)
	}
}

// 缓存必须避免重复抓取：数据新鲜期内不应再发请求。
func TestRuntimeProviderMetadataCachesRepeatedCalls(t *testing.T) {
	var hits int32
	var mu sync.Mutex
	port := providerTestServer(t, 0, &hits, &mu)

	svc := &service{
		cfg:    config.Config{RuntimeDir: t.TempDir()},
		agents: []model.Agent{{AgentID: "a", Status: "running", Port: port, Enabled: true}},
	}

	if _, err := svc.runtimeProviderMetadata(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	first := hits
	mu.Unlock()
	if first != 1 {
		t.Fatalf("首次抓取次数应为 1，实际 %d", first)
	}

	for i := 0; i < 3; i++ {
		if _, err := svc.runtimeProviderMetadata(); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	total := hits
	mu.Unlock()
	if total != 1 {
		t.Fatalf("新鲜缓存期内重复抓取了 %d 次", total)
	}
}

// 全部 agent 抓取失败时不能把空结果当成有效缓存，否则会固化暂时性故障。
func TestRuntimeProviderMetadataDoesNotCacheAllFailures(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	port, err := strconv.Atoi(strings.TrimPrefix(dead.URL, "http://127.0.0.1:"))
	if err != nil {
		t.Fatal(err)
	}
	dead.Close()

	svc := &service{
		cfg:    config.Config{RuntimeDir: t.TempDir()},
		agents: []model.Agent{{AgentID: "dead", Status: "running", Port: port, Enabled: true}},
	}
	metadata, err := svc.runtimeProviderMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if len(metadata.All) != 0 {
		t.Fatalf("抓取全失败却返回了元数据: %+v", metadata)
	}
	entry := svc.providerMetadataCache
	if entry == nil {
		t.Fatalf("缺少空快照，后续请求会反复等满抓取超时")
	}
	if time.Now().Before(entry.expires) {
		t.Fatalf("失败结果被写成了有效缓存，会固化暂时性故障")
	}

	// 第二次调用应立即返回，不再同步等待抓取。
	start := time.Now()
	if _, err := svc.runtimeProviderMetadata(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("失败后的重试仍在同步等待，耗时 %v", elapsed)
	}
}

// 后台刷新期间并发调用不应重复发起抓取。
func TestRuntimeProviderMetadataRefreshClaimsOnce(t *testing.T) {
	svc := &service{cfg: config.Config{RuntimeDir: t.TempDir()}}
	key := "4071"
	svc.storeProviderMetadataWithTTL(key, aiconfig.RuntimeProviderMetadata{
		All: []aiconfig.RuntimeProviderMetadataItem{{ID: "stale"}},
	}, -time.Second)

	if !svc.claimProviderMetadataRefresh(key) {
		t.Fatalf("首次认领刷新应成功")
	}
	if svc.claimProviderMetadataRefresh(key) {
		t.Fatalf("已有刷新在跑时不应再次认领")
	}
	svc.releaseProviderMetadataRefresh(key)
	if !svc.claimProviderMetadataRefresh(key) {
		t.Fatalf("释放后应可再次认领")
	}
	svc.releaseProviderMetadataRefresh(key)
}

// 过期数据仍应立即返回，并触发后台刷新（陈旧可用优于阻塞）。
func TestRuntimeProviderMetadataServesStaleDataImmediately(t *testing.T) {
	var hits int32
	var mu sync.Mutex
	port := providerTestServer(t, 0, &hits, &mu)

	svc := &service{
		cfg:    config.Config{RuntimeDir: t.TempDir()},
		agents: []model.Agent{{AgentID: "a", Status: "running", Port: port, Enabled: true}},
	}
	key := providerPortsKey([]int{port})
	svc.storeProviderMetadataWithTTL(key, aiconfig.RuntimeProviderMetadata{
		All: []aiconfig.RuntimeProviderMetadataItem{{ID: "stale-provider"}},
	}, -time.Second)

	start := time.Now()
	got, err := svc.runtimeProviderMetadata()
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("过期数据未立即返回，耗时 %v", elapsed)
	}
	if len(got.All) != 1 || got.All[0].ID != "stale-provider" {
		t.Fatalf("应返回旧数据，实际: %+v", got)
	}
	// 后台刷新应随后补齐真实数据。
	waitForProviderMetadata(t, svc, key)
}

// 非 running 的 agent 不应被请求。
func TestRuntimeProviderMetadataSkipsNonRunningAgents(t *testing.T) {
	var hits int32
	var mu sync.Mutex
	port := providerTestServer(t, 0, &hits, &mu)

	svc := &service{
		cfg: config.Config{RuntimeDir: t.TempDir()},
		agents: []model.Agent{
			{AgentID: "disabled", Status: "disabled", Port: port, Enabled: false},
			{AgentID: "stopped", Status: "stopped", Port: port, Enabled: false},
		},
	}
	metadata, err := svc.runtimeProviderMetadata()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	total := hits
	mu.Unlock()
	if total != 0 {
		t.Fatalf("对非 running agent 发起了 %d 次请求", total)
	}
	if len(metadata.All) != 0 {
		t.Fatalf("非 running agent 不应产生元数据: %+v", metadata)
	}
}
