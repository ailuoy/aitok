package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"strings"
	"unicode/utf8"
)

const maxEvidenceBytes = 8 << 20

type evidenceBlock struct {
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`
	Src     string `json:"src,omitempty"`
	Caption string `json:"caption,omitempty"`
}
type evidenceDocument struct {
	Format string          `json:"format"`
	Blocks []evidenceBlock `json:"blocks"`
}

// 凭据使用受限图文块，保存在现有 TEXT 字段；不接收或执行 HTML。
func validateEvidence(value string) error {
	if len(value) > maxEvidenceBytes {
		return fmt.Errorf("凭据总大小不能超过 8 MB")
	}
	if !strings.HasPrefix(strings.TrimSpace(value), "{") {
		if utf8.RuneCountInString(value) > 4000 {
			return fmt.Errorf("文字凭据不能超过 4000 字")
		}
		return nil
	}
	var document evidenceDocument
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&document) != nil || decoder.Decode(&struct{}{}) != io.EOF || document.Format != "aitok-evidence-v1" || len(document.Blocks) > 40 {
		return fmt.Errorf("图文凭据格式无效")
	}
	images, characters, content := 0, 0, false
	for _, block := range document.Blocks {
		switch block.Type {
		case "text":
			if block.Src != "" || block.Caption != "" {
				return fmt.Errorf("文字块格式无效")
			}
			characters += utf8.RuneCountInString(block.Text)
			content = content || strings.TrimSpace(block.Text) != ""
		case "image":
			images++
			if block.Text != "" || utf8.RuneCountInString(block.Caption) > 200 {
				return fmt.Errorf("图片说明无效")
			}
			if err := validateImageDataURL(block.Src); err != nil {
				return err
			}
			content = true
		default:
			return fmt.Errorf("不支持的凭据内容类型")
		}
	}
	if images > 6 || characters > 4000 || !content {
		return fmt.Errorf("凭据需包含文字或图片，最多 4000 字、6 张图片")
	}
	return nil
}

// 资金流水只保存可读摘要，完整凭据在订单及对应操作记录中读取。
func evidenceSummary(value string) string {
	var document evidenceDocument
	if json.Unmarshal([]byte(value), &document) != nil || document.Format != "aitok-evidence-v1" {
		return value
	}
	var parts []string
	for _, block := range document.Blocks {
		if block.Type == "text" {
			parts = append(parts, block.Text)
		} else {
			parts = append(parts, "[凭据图片] "+block.Caption)
		}
	}
	runes := []rune(strings.Join(parts, "\n"))
	if len(runes) > 3500 {
		runes = runes[:3500]
	}
	return string(runes)
}

// 凭据与钱包二维码共用图片内容、大小及分辨率校验。
func validateImageDataURL(value string) error {
	header, encoded, ok := strings.Cut(value, ",")
	mime := strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64")
	if !ok || header != "data:"+mime+";base64" || (mime != "image/png" && mime != "image/jpeg" && mime != "image/gif") {
		return fmt.Errorf("仅支持 PNG、JPEG、GIF 的 Base64 图片")
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(decoded) == 0 || len(decoded) > 2<<20 {
		return fmt.Errorf("图片内容无效或超过 2 MB")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(decoded))
	if err != nil || "image/"+format != mime || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 20000000 {
		return fmt.Errorf("图片格式或分辨率无效")
	}
	return nil
}
