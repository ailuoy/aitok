package main

import (
	"os"
	"testing"
)

func TestOrderLifecycleBackfill(t *testing.T) {
	db := walletTestDB(t)
	_, err := db.Exec(`INSERT INTO recharge_orders(id,order_no,user_id,account_id,account_email,package_id,package_snapshot,period_start,period_end,sale_usd_minor,request_key,payment_status,fulfillment_status,cost_usd_minor) VALUES
(1,'old-refund',1,1,'one@test.local',1,'{}','2030-01-01','2030-02-01',20000,'old-refund','paid','completed',15000),
(2,'old-cancel',1,2,'two@test.local',1,'{}','2030-01-01','2030-02-01',20000,'old-cancel','unpaid','cancelled',0),
(3,'old-paid-refund',1,3,'three@test.local',1,'{}','2030-01-01','2030-02-01',20000,'old-paid-refund','refunded','completed',15000),
(4,'old-active',1,4,'four@test.local',1,'{}','2030-01-01','2030-02-01',20000,'old-active','paid','completed',15000);
INSERT INTO operation_events(actor_id,entity_type,entity_id,action,request_key,after_data) VALUES(1,'order',1,'refund_note','old-refund-note','{"input":{"reason":"旧退款","evidence":"凭据"}}');`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile("../../migrations/020_order_lifecycle_backfill.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = db.Exec(string(script)); err != nil {
			t.Fatal(err)
		}
		var statuses string
		var versions int
		var cost, refunded int64
		err = db.QueryRow(`SELECT string_agg(order_status,',' ORDER BY id),sum(version),sum(cost_usd_minor),sum(refunded_usd_minor) FROM recharge_orders`).Scan(&statuses, &versions, &cost, &refunded)
		if err != nil || statuses != "refunded,discarded,refunded,active" || versions != 3 || cost != 45000 || refunded != 0 {
			t.Fatal(statuses, versions, cost, refunded, err)
		}
	}
}
