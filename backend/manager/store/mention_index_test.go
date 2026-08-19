package store

import "testing"

func TestInvalidateGlobalMentionIndex(t *testing.T) {
	s := &Store{}
	s.globalMentionIndex = &GlobalMentionIndex{}
	s.InvalidateGlobalMentionIndex()
	if s.globalMentionIndex != nil {
		t.Fatal("InvalidateGlobalMentionIndex should clear the cached index")
	}
}
