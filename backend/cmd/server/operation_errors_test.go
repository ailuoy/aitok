package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestOperationErrorSafeMessages(t *testing.T) {
	for _, test := range []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"missing", sql.ErrNoRows, 404, "记录不存在或无权访问"},
		{"business", fmt.Errorf("wrapped: %w", operationConflict("付款卡可用 USD 余额不足")), 409, "付款卡可用 USD 余额不足"},
		{"duplicate transaction", fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: "23505", ConstraintName: "bank_card_ledger_reference_unique", Detail: "private-reference"}), 409, "同一笔交易不能重复录入"},
		{"duplicate purchase", &pgconn.PgError{Code: "23505", ConstraintName: "recharge_orders_purchase_reference_unique"}, 409, "同一笔交易不能重复录入"},
		{"duplicate order", &pgconn.PgError{Code: "23505", ConstraintName: "bank_card_ledger_order_unique"}, 409, "此订单已扣款"},
		{"duplicate cycle", &pgconn.PgError{Code: "23505", ConstraintName: "recharge_orders_cycle_unique"}, 409, "重叠周期"},
		{"unknown database error", &pgconn.PgError{Code: "23514", ConstraintName: "private-constraint", Detail: "private-row"}, 409, "操作未完成"},
		{"unknown unique index", &pgconn.PgError{Code: "23505", ConstraintName: "private-index", Detail: "private-row"}, 409, "操作未完成"},
		{"internal error", errors.New("private-token"), 409, "操作未完成"},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			operationError(w, test.err)
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if w.Code != test.status || !strings.Contains(body["error"], test.message) || strings.Contains(w.Body.String(), "private-") {
				t.Fatalf("unexpected response: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
