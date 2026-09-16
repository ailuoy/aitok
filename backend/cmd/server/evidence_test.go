package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"math/rand"
	"strings"
	"testing"
)

func testRichEvidence(t *testing.T) string {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 128, 128))
	random := rand.New(rand.NewSource(1))
	_, _ = random.Read(picture.Pix)
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, picture); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(evidenceDocument{Format: "aitok-evidence-v1", Blocks: []evidenceBlock{
		{Type: "text", Text: "交易说明"},
		{Type: "image", Src: "data:image/png;base64," + base64.StdEncoding.EncodeToString(buffer.Bytes()), Caption: "账单截图"},
		{Type: "text", Text: "已核对"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestEvidenceValidation(t *testing.T) {
	valid := testRichEvidence(t)
	if len(valid) <= 16384 {
		t.Fatal("图片样本需超过旧请求上限")
	}
	if err := validateEvidence(valid); err != nil {
		t.Fatal(err)
	}
	if summary := evidenceSummary(valid); !strings.Contains(summary, "账单截图") || strings.Contains(summary, "base64") {
		t.Fatal("流水需保留文字摘要")
	}
	for name, value := range map[string]string{
		"空内容":     `{"format":"aitok-evidence-v1","blocks":[{"type":"text","text":" "}]}`,
		"外部图片":    `{"format":"aitok-evidence-v1","blocks":[{"type":"image","src":"https://example.com/private.png"}]}`,
		"HTML内容块": `{"format":"aitok-evidence-v1","blocks":[{"type":"html","text":"<script>alert(1)</script>"}]}`,
		"脚本图片":    strings.Replace(valid, "data:image/png;base64,", "data:image/svg+xml;base64,", 1),
		"未知字段":    strings.Replace(valid, `"caption":`, `"onerror":"alert(1)","caption":`, 1),
		"损坏图片":    `{"format":"aitok-evidence-v1","blocks":[{"type":"image","src":"data:image/png;base64,aGVsbG8="}]}`,
		"超大文档":    strings.Repeat("x", maxEvidenceBytes+1),
		"超长文字":    strings.Repeat("文", 4001),
	} {
		t.Run(name, func(t *testing.T) {
			if validateEvidence(value) == nil {
				t.Fatal("无效凭据被接受")
			}
		})
	}
	if err := validateEvidence("原有纯文本凭据"); err != nil {
		t.Fatal(err)
	}
}
