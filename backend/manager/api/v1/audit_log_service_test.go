package v1

import "testing"

// TestAuditLogFindFromRequest verifies filter/order parsing and rejection of
// unsupported fields, operators, and orderings.
func TestAuditLogFindFromRequest(t *testing.T) {
	find, err := auditLogFindFromRequest(
		`method = "/laelia.v1.IamService/SetWorkspaceIamPolicy" && actor = "admin@example.com" && status = "ok"`,
		"create_time asc",
		50, 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !find.OrderAsc {
		t.Fatal("expected ascending order")
	}
	if find.Method == nil || *find.Method != "/laelia.v1.IamService/SetWorkspaceIamPolicy" {
		t.Fatalf("unexpected method filter: %+v", find.Method)
	}
	if find.ActorID == nil || *find.ActorID != "admin@example.com" {
		t.Fatalf("unexpected actor filter: %+v", find.ActorID)
	}
	if find.Status == nil || *find.Status != "ok" {
		t.Fatalf("unexpected status filter: %+v", find.Status)
	}
	if find.Limit == nil || *find.Limit != 50 {
		t.Fatalf("unexpected limit: %+v", find.Limit)
	}

	if _, err := auditLogFindFromRequest(`project = "x"`, "", 10, 0); err == nil {
		t.Fatal("expected unsupported filter field to be rejected")
	}
	if _, err := auditLogFindFromRequest("", "create_time mid", 10, 0); err == nil {
		t.Fatal("expected unsupported order_by to be rejected")
	}
	if _, err := auditLogFindFromRequest(`method != "x"`, "", 10, 0); err == nil {
		t.Fatal("expected non-equality operator to be rejected")
	}
}

// TestAuditLogFindFromRequestDefaults verifies the default ordering and empty
// filter produce a valid find message.
func TestAuditLogFindFromRequestDefaults(t *testing.T) {
	find, err := auditLogFindFromRequest("", "", 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if find.OrderAsc {
		t.Fatal("default order must be descending")
	}
	if find.Method != nil || find.ActorID != nil || find.Resource != nil || find.Status != nil {
		t.Fatalf("expected no filters, got %+v", find)
	}
}
