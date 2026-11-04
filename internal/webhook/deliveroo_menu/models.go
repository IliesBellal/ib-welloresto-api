package deliveroo_menu

import "welloresto-api/internal/modules/deliveroo"

// EventMenuUploadResult est le seul événement du webhook menu Deliveroo.
const EventMenuUploadResult = "menu.upload_result"

// MenuWebhookPayload est le corps du webhook menu Deliveroo
// (api-docs.deliveroo.com/reference/menu-events-webhook) :
//
//	{"event": "menu.upload_result",
//	 "body": {"menu_upload_result": {"http_status": 200, "brand_id": "...",
//	          "menu_id": "...", "site_ids": ["..."], "errors": {...}}}}
type MenuWebhookPayload struct {
	Event string          `json:"event"`
	Body  MenuWebhookBody `json:"body"`
}

type MenuWebhookBody struct {
	MenuUploadResult deliveroo.MenuUploadResult `json:"menu_upload_result"`
}
