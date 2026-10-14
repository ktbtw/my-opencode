package promptregistry

import "testing"

func TestSlotsHaveDefaultsAndIDs(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("槽位清单为空")
	}
	for _, slot := range all {
		if slot.ID == "" || slot.Category == "" || len(slot.Scope) == 0 {
			t.Fatalf("槽位定义不完整: %+v", slot)
		}
		if Default(slot.ID) == "" {
			t.Fatalf("槽位缺少内置默认文本: %s", slot.ID)
		}
	}
}

func TestByIDAndScope(t *testing.T) {
	slot, ok := ByID("chat.base_prompt")
	if !ok {
		t.Fatal("未找到 chat.base_prompt")
	}
	for _, scope := range []string{ScopePlatform, ScopeSemantic, ScopeDevice} {
		if !SupportsScope(slot, scope) {
			t.Fatalf("chat.base_prompt 应支持 %s 覆盖", scope)
		}
	}
	if _, ok := ByID("unknown.slot"); ok {
		t.Fatal("未知槽位不应命中")
	}
}

func TestSanitizeOverridesDropsUnknownAndEmpty(t *testing.T) {
	out := SanitizeOverrides(ScopeSemantic, map[string]string{
		"chat.base_prompt":     "自定义",
		"unknown.slot":         "x",
		"chat.question_policy": "   ",
	})
	if len(out) != 1 || out["chat.base_prompt"] != "自定义" {
		t.Fatalf("清洗结果不符合预期: %+v", out)
	}
}

func TestMissingVariables(t *testing.T) {
	slot := Slot{ID: "job.memory_curator", Variables: []string{"schema"}}
	if got := MissingVariables(slot, "请输出 {{schema}} 结构"); len(got) != 0 {
		t.Fatalf("不应报告缺失: %v", got)
	}
	if got := MissingVariables(slot, "请输出结构"); len(got) != 1 || got[0] != "schema" {
		t.Fatalf("应报告缺失 schema: %v", got)
	}
}
