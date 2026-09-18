package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var adminPagePattern = regexp.MustCompile(`^/admin/(accounts|proxies|addresses|bank-cards|users|wallet|orders|packages|notices|audit|proxy-activity|payment-exceptions)(/[0-9]+)?$`)
var auditControls = map[string]string{
	"browser_fingerprint": "重新生成浏览器指纹",
	"button":              "点击按钮", "link": "打开链接", "form": "提交表单", "select": "选择选项", "toggle": "切换选项",
	"add": "新增", "edit": "编辑", "delete": "删除", "confirm": "确认操作", "cancel": "取消操作", "save": "保存",
	"search": "搜索", "refresh": "刷新", "export": "导出", "import": "导入", "open_browser": "打开浏览器", "close_browser": "关闭浏览器",
	"view": "查看详情", "copy": "复制", "theme": "切换主题", "paginate": "翻页", "test": "测试代理", "get_ip": "获取出口 IP",
	"assign": "分配处理人", "verify": "核验开通", "refund": "退款", "deposit": "记录存入", "purchase": "记录官网扣款", "group": "管理分组",
	"subscription": "修改是否续订", "proxy_create": "新增本机代理", "proxy_update": "编辑本机代理", "proxy_delete": "删除本机代理",
	"discard":    "废弃订单",
	"bind_user":  "绑定账号所属用户",
	"proxy_read": "查看本机代理", "proxy_test": "测试本机代理", "proxy_import": "解析代理导入", "proxy_bind": "绑定账号代理",
}

type adminActivity struct {
	Key     string `json:"request_key"`
	Page    string `json:"page"`
	Kind    string `json:"kind"`
	Control string `json:"control"`
	Result  string `json:"result"`
}

// 客户端只允许提交固定动作枚举；身份、时间及结果来源由服务端确定。
// 不接受表单值、按钮原文、查询参数、请求正文或客户端指定的操作者。
func (s *Server) adminActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	user, err := s.auth(r)
	if err != nil {
		w.WriteHeader(401)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2048)
	var in adminActivity
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&in) != nil || decoder.Decode(&struct{}{}) != io.EOF || !ledgerKeyPattern.MatchString(in.Key) || !adminPagePattern.MatchString(in.Page) {
		w.WriteHeader(400)
		return
	}
	action := auditControls[in.Control]
	switch in.Kind {
	case "page_view":
		if in.Control != "" || in.Result != "visited" {
			w.WriteHeader(400)
			return
		}
		action = "访问页面"
	case "click", "submit", "change":
		if action == "" || in.Result != "triggered" {
			w.WriteHeader(400)
			return
		}
	case "local_request":
		if action == "" || !(strings.HasPrefix(in.Control, "proxy_") || in.Control == "open_browser" || in.Control == "close_browser" || in.Control == "browser_fingerprint") || (in.Result != "success" && in.Result != "failure" && in.Result != "cancelled") {
			w.WriteHeader(400)
			return
		}
	default:
		w.WriteHeader(400)
		return
	}
	if !s.allowAttempt(r.Context(), "admin-activity:"+strconv.FormatInt(user, 10), 300, time.Minute) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
		return
	}
	data, _ := json.Marshal(map[string]any{"source": "browser", "page": in.Page, "kind": in.Kind, "control": in.Control, "result": in.Result})
	var id int64
	err = s.db.QueryRowContext(r.Context(), `INSERT INTO operation_events(actor_id,entity_type,entity_id,action,request_key,after_data) VALUES($1,'admin_ui',$1,$2,$3,$4) ON CONFLICT(entity_type,entity_id,request_key) DO NOTHING RETURNING id`, user, action, in.Key, data).Scan(&id)
	if err == sql.ErrNoRows {
		var same bool
		err = s.db.QueryRowContext(r.Context(), `SELECT after_data=$3::jsonb FROM operation_events WHERE entity_type='admin_ui' AND entity_id=$1 AND request_key=$2`, user, in.Key, data).Scan(&same)
		if err == nil && !same {
			reply(w, map[string]string{"error": "审计请求标识已被其他操作使用"}, 409)
			return
		}
	}
	if err != nil {
		reply(w, map[string]string{"error": "操作记录保存失败"}, 503)
		return
	}
	w.WriteHeader(204)
}

type requestAuditContext struct {
	Page, Method, Resource string
	Recorded               bool
}
type requestAuditKey struct{}

func (s *Server) auditFilter(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.db == nil || !strings.HasPrefix(r.URL.Path, "/api/") || r.Method == "OPTIONS" || r.URL.Path == "/api/admin-activity" || r.URL.Path == "/api/stripe/webhook" {
			next.ServeHTTP(w, r)
			return
		}
		user, err := s.auth(r)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		page := r.Header.Get("X-Aitok-Page")
		if !adminPagePattern.MatchString(page) {
			page = ""
		}
		resource := safeAuditResource(r.URL.Path)
		ctx := &requestAuditContext{Page: page, Method: r.Method, Resource: resource}
		r = r.WithContext(context.WithValue(r.Context(), requestAuditKey{}, ctx))
		aw := &auditWriter{ResponseWriter: w}
		next.ServeHTTP(aw, r)
		status := aw.status
		if status == 0 {
			status = 200
		}
		mutation := r.Method != "GET" && r.Method != "HEAD"
		// 定时同步与页面轮询不制造操作噪音；用户主动访问、筛选由前端事件覆盖。
		background := r.URL.Path == "/api/proxy-activity" || strings.HasSuffix(r.URL.Path, "/login")
		sensitiveRead := strings.HasSuffix(r.URL.Path, "/export") || (strings.HasPrefix(r.URL.Path, "/api/card-operations/") && r.URL.Query().Get("export") == "1") || auditCardDetails.MatchString(r.URL.Path) || (r.URL.Path == "/api/bank-cards" && r.URL.Query().Get("include_numbers") == "1")
		if ctx.Recorded && status < 400 {
			return
		} // 业务事务内已有完整审计，不重复追加成功记录。
		if (!mutation || background) && !sensitiveRead && !(page != "" && status >= 400) {
			return
		}
		result := "success"
		if status >= 400 {
			result = "failure"
		}
		payload, _ := json.Marshal(map[string]any{"source": "server", "page": page, "method": r.Method, "resource": resource, "status": status, "result": result})
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Second)
		defer cancel()
		_, err = s.db.ExecContext(writeCtx, `INSERT INTO operation_events(actor_id,entity_type,entity_id,action,request_key,after_data) VALUES($1,'admin_request',0,$2,$3,$4)`, user, r.Method+" "+resource, eventKey(), payload)
		if err != nil {
			log.Print("operation audit write failed")
		}
	})
}

// 未知路径可能夹带凭据，只保留已知资源、数字 ID 和固定动作段。
func safeAuditResource(path string) string {
	known := map[string]bool{"api": true, "accounts": true, "account-groups": true, "addresses": true, "bank-cards": true, "card-operations": true, "orders": true, "packages": true, "users": true, "wallet": true, "topups": true, "audit": true, "proxy-activity": true, "payment-exceptions": true, "notices": true, "order-operators": true, "me": true, "logout": true, "export": true, "import": true, "ledger": true, "access": true, "role": true, "group": true, "login": true, "subscription": true, "browser": true, "browser-session": true, "session": true, "renewal-date": true, "renew": true, "sync": true, "refund": true, "collection-quote": true, "record": true}
	known["billing-address"] = true
	known["payment-card"], known["payment-cards"] = true, true
	known["owner"] = true
	known["desktop-auth"] = true
	known["notes"] = true
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, part := range parts {
		if !known[part] && !auditNumericID.MatchString(part) {
			parts[i] = ":id"
		}
	}
	return "/" + strings.Join(parts, "/")
}

var auditNumericID = regexp.MustCompile(`^[0-9]{1,19}$`)

var auditCardDetails = regexp.MustCompile(`^/api/bank-cards/[0-9]+$`)
