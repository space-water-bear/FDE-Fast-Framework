package codegen

import (
	"strings"
	"testing"
)

func TestManagedQueryAndFormUseTheSameJSONFieldNames(t *testing.T) {
	data, err := Prepare(GenRequest{TableName: "biz_repair_ticket", Business: "维修登记", Fields: []GenField{{Name: "ticket_no", Title: "登记号", Type: "string", Search: true, InList: true, InForm: true}}})
	if err != nil {
		t.Fatal(err)
	}
	files, err := RenderBusiness(data)
	if err != nil {
		t.Fatal(err)
	}
	var api, view string
	for _, file := range files {
		if file.Path == "internal/api/repair_ticket.go" {
			api = file.Content
		}
		if strings.HasSuffix(file.Path, "index.vue") {
			view = file.Content
		}
	}
	if !strings.Contains(api, `form:"ticketNo"`) || !strings.Contains(view, "params.ticketNo = query.ticketNo") {
		t.Fatal("managed backend and frontend query contract differ")
	}
	legacy, err := Render(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range legacy {
		if file.Path == "internal/api/repair_ticket.go" && !strings.Contains(file.Content, `form:"ticket_no"`) {
			t.Fatal("legacy query contract changed")
		}
		if strings.HasSuffix(file.Path, "index.vue") && !strings.Contains(file.Content, "params.ticket_no = query.ticketNo") {
			t.Fatal("legacy frontend query contract differs from backend")
		}
	}
}
