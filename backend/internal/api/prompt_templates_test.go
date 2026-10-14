package api

import "testing"

func TestPromptTemplatesRevisionStableAndSensitive(t *testing.T) {
	base := map[string]string{"chat.base_prompt": "A", "chat.question_policy": ""}
	first := promptTemplatesRevision(base)
	if first != promptTemplatesRevision(map[string]string{"chat.question_policy": "", "chat.base_prompt": "A"}) {
		t.Fatal("revision 应与会话无关地稳定")
	}
	if first == promptTemplatesRevision(map[string]string{"chat.base_prompt": "B", "chat.question_policy": ""}) {
		t.Fatal("内容变化时 revision 必须变化")
	}
	if len(first) != 16 {
		t.Fatalf("revision 长度应为 16，实际 %d", len(first))
	}
}
