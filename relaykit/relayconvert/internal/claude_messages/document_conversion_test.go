package claudemessages

import (
	"context"
	"strings"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/relaykit/dto"
	"github.com/zzyyyds88/PowerBarRations/relaykit/relayconvert/internal/convdiag"
)

// Claude document 内容块按 new-api 口径转换：
//   - source.type=text               → Chat text part
//   - source.type=base64 + PDF       → Chat file part（data URL）
//   - 其它（含 file_id 引用、非 PDF）→ 记 unsupported_media_source 诊断并略过
func TestClaudeDocumentConversion(t *testing.T) {
	cases := []struct {
		name       string
		source     *dto.ClaudeMessageSource
		wantType   string
		wantText   string
		wantData   string
		wantDiagCd string
	}{
		{
			name:     "plain text document becomes text part",
			source:   &dto.ClaudeMessageSource{Type: "text", Data: "hello from doc"},
			wantType: "text",
			wantText: "hello from doc",
		},
		{
			name:     "base64 pdf becomes file part",
			source:   &dto.ClaudeMessageSource{Type: "base64", MediaType: "application/pdf", Data: "JVBERi0="},
			wantType: dto.ContentTypeFile,
			wantData: "data:application/pdf;base64,JVBERi0=",
		},
		{
			name:       "file reference is omitted with diagnostic",
			source:     &dto.ClaudeMessageSource{Type: "file"},
			wantDiagCd: "unsupported_media_source",
		},
		{
			name:       "non-pdf base64 is omitted with diagnostic",
			source:     &dto.ClaudeMessageSource{Type: "base64", MediaType: "text/csv", Data: "YSxi"},
			wantDiagCd: "unsupported_media_source",
		},
		{
			name:       "nil source is omitted with diagnostic",
			source:     nil,
			wantDiagCd: "unsupported_media_source",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := dto.ClaudeRequest{
				Model:     "m",
				MaxTokens: ptrUint(16),
				Messages: []dto.ClaudeMessage{{
					Role: "user",
					Content: []any{
						map[string]any{"type": "document", "source": tc.source},
					},
				}},
			}

			ctx, collector := convdiag.WithCollector(context.Background())
			out, err := ClaudeMessagesRequestToOpenAIChat(ctx, request, nil)
			if err != nil {
				t.Fatalf("convert: %v", err)
			}
			diagnostics := collector.Diagnostics()

			if tc.wantDiagCd != "" {
				// 与 new-api 一致：整条消息没有可发送内容时整条丢弃（诊断已记录）。
				if len(out.Messages) != 0 {
					t.Fatalf("want message dropped, got %+v", out.Messages)
				}
				if len(diagnostics) == 0 {
					t.Fatalf("want diagnostic, got none")
				}
				if diagnostics[0].Code != tc.wantDiagCd {
					t.Fatalf("want code %q, got %q", tc.wantDiagCd, diagnostics[0].Code)
				}
				if diagnostics[0].Path != "messages[0].content[0].source" {
					t.Fatalf("want diagnostic path, got %q", diagnostics[0].Path)
				}
				return
			}

			if len(out.Messages) != 1 {
				t.Fatalf("want 1 message, got %d", len(out.Messages))
			}
			parts := out.Messages[0].ParseContent()
			if len(diagnostics) != 0 {
				t.Fatalf("want no diagnostics, got %+v", diagnostics)
			}
			if len(parts) != 1 {
				t.Fatalf("want 1 content part, got %d: %+v", len(parts), parts)
			}
			if parts[0].Type != tc.wantType {
				t.Fatalf("want part type %q, got %q", tc.wantType, parts[0].Type)
			}
			if tc.wantText != "" && parts[0].Text != tc.wantText {
				t.Fatalf("want text %q, got %q", tc.wantText, parts[0].Text)
			}
			if tc.wantData != "" {
				file, ok := parts[0].File.(*dto.MessageFile)
				if !ok {
					t.Fatalf("want *dto.MessageFile, got %T", parts[0].File)
				}
				if !strings.HasPrefix(file.FileData, tc.wantData) {
					t.Fatalf("want file data prefix %q, got %q", tc.wantData, file.FileData)
				}
				if file.FileName != "document.pdf" {
					t.Fatalf("want file name document.pdf, got %q", file.FileName)
				}
			}
		})
	}
}

// Claude image 的 source 可能缺失（转写/代理截断），转换器不得 panic，
// 且必须留下可诊断的 unsupported_media_source。
func TestClaudeImageSourceMissingIsDiagnosed(t *testing.T) {
	request := dto.ClaudeRequest{
		Model:     "m",
		MaxTokens: ptrUint(16),
		Messages: []dto.ClaudeMessage{{
			Role:    "user",
			Content: []any{map[string]any{"type": "image", "source": nil}},
		}},
	}
	ctx, collector := convdiag.WithCollector(context.Background())
	out, err := ClaudeMessagesRequestToOpenAIChat(ctx, request, nil)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if len(out.Messages) != 0 {
		t.Fatalf("want image-only message dropped, got %+v", out.Messages)
	}
	diagnostics := collector.Diagnostics()
	if len(diagnostics) == 0 {
		t.Fatalf("want diagnostic, got none")
	}
	if diagnostics[0].Code != "unsupported_media_source" {
		t.Fatalf("want code unsupported_media_source, got %q", diagnostics[0].Code)
	}
}

func ptrUint(v uint) *uint { return &v }
