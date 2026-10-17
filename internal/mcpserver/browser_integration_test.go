package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/drumandbytes/eraser/internal/browser"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func demoTestChrome(t *testing.T) string {
	t.Helper()
	if os.Getenv("ERASER_TEST_NO_CHROME") != "" {
		t.Skip("ERASER_TEST_NO_CHROME is set, skipping Chrome-dependent test")
	}
	path := os.Getenv("ERASER_TEST_CHROME")
	if os.Getenv("GITHUB_ACTIONS") != "true" || runtime.GOOS != "linux" {
		return path
	}
	// Match the existing browser test setup for Ubuntu runners, which cannot
	// provide Chrome's sandbox. This wrapper is used only with local fixtures.
	if path == "" {
		for _, name := range []string{"google-chrome", "chromium", "chromium-browser"} {
			if found, err := exec.LookPath(name); err == nil {
				path = found
				break
			}
		}
	}
	if path == "" {
		t.Fatal("Chrome is required on CI")
	}
	wrapper := filepath.Join(t.TempDir(), "chrome-for-mcp-tests")
	script := "#!/bin/sh\nexec '" + strings.ReplaceAll(path, "'", "'\"'\"'") + "' --no-sandbox \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return wrapper
}

// Drive the same schema-validated MCP calls used by Codex, with real Chrome
// and local fake brokers. No model subscription or real mailbox is required.
func TestDemoBrowserWorkflowOverMCP(t *testing.T) {
	s, cleanup, err := NewDemo(true, demoTestChrome(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(filepath.Dir(s.opts.ConfigPath)) }()
	defer cleanup()
	defer func() { _ = s.Close() }()
	ctx := context.Background()
	a, b := mcp.NewInMemoryTransports()
	ss, err := s.MCP.Connect(ctx, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ss.Close() }()
	client := mcp.NewClient(&mcp.Implementation{Name: "codex-workflow-test", Version: "1"}, nil)
	cs, err := client.Connect(ctx, b, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cs.Close() }()
	call := func(name string, args any) map[string]any {
		t.Helper()
		r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if r.IsError {
			msg, _ := json.Marshal(r)
			if name == "open_task" && os.Getenv("ERASER_TEST_CHROME") == "" && os.Getenv("GITHUB_ACTIONS") != "true" && strings.Contains(string(msg), "start Chrome") {
				t.Skipf("Chrome unavailable: %s", msg)
			}
			t.Fatalf("%s: %s", name, msg)
		}
		out, ok := r.StructuredContent.(map[string]any)
		if !ok {
			t.Fatalf("missing structured result: %+v", r)
		}
		if name == "open_task" && out["page"] == nil {
			msg := fmt.Sprint(out["needs_user"])
			if os.Getenv("ERASER_TEST_CHROME") == "" && os.Getenv("GITHUB_ACTIONS") != "true" && strings.Contains(msg, "start Chrome") {
				t.Skipf("Chrome unavailable: %s", msg)
			}
			t.Fatalf("open task needs intervention: %s", msg)
		}
		return out
	}
	decodePage := func(out map[string]any) *browser.Snapshot {
		t.Helper()
		b, err := json.Marshal(out["page"])
		if err != nil {
			t.Fatal(err)
		}
		var p browser.Snapshot
		if err := json.Unmarshal(b, &p); err != nil {
			t.Fatal(err)
		}
		return &p
	}
	for _, tc := range []struct {
		id           string
		steps        bool
		confirmation bool
		reject       bool
	}{{"task:1", false, false, false}, {"task:2", true, false, false}, {"task:4", false, true, false}, {"task:5", false, false, true}} {
		t.Run(tc.id, func(t *testing.T) {
			preview := call("prepare_batch", prepareInput{TaskIDs: []string{tc.id}})
			approveTestPlan(t, s, preview["plan_id"].(string))
			batch := call("begin_batch", beginInput{preview["plan_id"].(string)})["batch_id"].(string)
			page := decodePage(call("open_task", openInput{batch, tc.id}))
			fill := func(label, field, value string) {
				t.Helper()
				ref := ""
				for _, c := range page.Controls {
					if c.Label == label {
						ref = c.Ref
						break
					}
				}
				if ref == "" {
					t.Fatalf("missing %s in %+v", label, page.Controls)
				}
				page = decodePage(call("browser_action", actionInput{BatchID: batch, TaskID: tc.id, SnapshotID: page.ID, Ref: ref, Action: "fill", ProfileField: field, Value: value}))
			}
			click := func(label string) {
				t.Helper()
				ref := ""
				for _, c := range page.Controls {
					if c.Label == label {
						ref = c.Ref
						break
					}
				}
				if ref == "" {
					t.Fatalf("missing %s", label)
				}
				page = decodePage(call("browser_action", actionInput{BatchID: batch, TaskID: tc.id, SnapshotID: page.ID, Ref: ref, Action: "click"}))
			}
			if !tc.confirmation {
				if tc.id == "task:1" {
					click("Submit request")
					if !page.SubmissionBlocked || len(page.Validation) == 0 {
						t.Fatal("missing native form validation")
					}
					call("record_result", resultInput{batch, tc.id, page.ID, "progress", page.Validation[0]})
					fill("First name", "first_name", "")
				}
				fill("Email", "email", "")
				if tc.steps {
					click("Next step")
					call("record_result", resultInput{batch, tc.id, page.ID, "progress", "Step two"})
					fill("City", "city", "")
				}
				if tc.id == "task:1" {
					fill("Country", "country", "")
					fill("I request deletion of my personal data", "", "true")
				}
				click("Submit request")
			}
			if tc.reject {
				r, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "record_result", Arguments: resultInput{batch, tc.id, page.ID, "submitted", "Your request was not submitted."}})
				if err != nil || !r.IsError {
					t.Fatalf("rejected form accepted: %+v %v", r, err)
				}
				call("record_result", resultInput{batch, tc.id, page.ID, "unknown", "Your request was not submitted."})
			} else {
				evidence := "Your request has been received."
				if tc.confirmation {
					evidence = "Your request has been confirmed."
				}
				// Screenshots are native MCP image content, not arbitrary file access.
				image, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "get_screenshot", Arguments: openInput{batch, tc.id}})
				if err != nil || image.IsError || len(image.Content) < 2 {
					t.Fatalf("screenshot: %+v %v", image, err)
				}
				call("record_result", resultInput{batch, tc.id, page.ID, "submitted", evidence})
			}
			call("end_batch", batchInput{batch})
		})
	}
	done, err := s.store.GetPendingTasks("default", "", "completed")
	if err != nil || len(done) != 3 {
		t.Fatalf("UI must have three completed tasks: %+v %v", done, err)
	}
	for _, task := range done {
		if task.ScreenshotPath == "" || !strings.Contains(task.Notes, "does not establish") {
			t.Fatalf("missing submission evidence: %+v", task)
		}
	}
}
