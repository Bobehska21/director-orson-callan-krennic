package issues

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"sort"
	"testing"

	"github.com/acme/krennic/internal/model"
)

func TestLabelsForClassifiesBackendFrontend(t *testing.T) {
	ev := model.ChangeEvent{
		Diff: model.Diff{Hunks: []model.Hunk{
			{Path: "internal/api/server.go", Language: "go"},
			{Path: "web/App.tsx", Language: "typescript"},
		}},
	}
	got := labelsFor(ev)
	sort.Strings(got)
	want := []string{"backend", "bug", "frontend", "krennic"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("labelsFor() = %#v, want %#v", got, want)
	}
}

func TestGitHubReporterCreatesIssueForRequestChanges(t *testing.T) {
	var paths []string
	var issueBody map[string]any
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		if r.Method == http.MethodPost && r.URL.Path == "/repos/acme/payments/labels" {
			return response(http.StatusCreated, ""), nil
		}
		if r.Method == http.MethodPost && r.URL.Path == "/repos/acme/payments/issues" {
			if err := json.NewDecoder(r.Body).Decode(&issueBody); err != nil {
				t.Fatalf("decode issue body: %v", err)
			}
			return response(http.StatusCreated, ""), nil
		}
		t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		return response(http.StatusNotFound, ""), nil
	})}

	r := &githubReporter{token: "tok", client: client, base: "https://api.github.test"}
	ev := model.ChangeEvent{
		ChangeID: "change-1",
		Developer: model.Developer{
			GitName:  "Alice",
			GitEmail: "alice@example.com",
		},
		Repo: model.Repo{
			Name:    "payments",
			Remote:  "https://github.com/acme/payments.git",
			Branch:  "feature-x",
			HeadSHA: "abc123",
		},
		Summary: model.Summary{Languages: []string{"go"}},
		Diff: model.Diff{Hunks: []model.Hunk{
			{Path: "internal/payments/service.go", Language: "go"},
		}},
	}
	rv := &model.ReviewResult{
		Verdict: "request-changes",
		Summary: "nil pointer risk",
		Findings: []model.Finding{{
			Path: "internal/payments/service.go", Line: 42, Severity: "high", Type: "logic",
			Message: "missing nil check", Suggestion: "check err before dereference",
		}},
	}
	if err := r.Report(context.Background(), ev, rv); err != nil {
		t.Fatalf("Report() error = %v", err)
	}
	if issueBody["title"] == "" {
		t.Fatal("issue title was empty")
	}
	labels, ok := issueBody["labels"].([]any)
	if !ok || len(labels) == 0 {
		t.Fatalf("issue labels missing: %#v", issueBody["labels"])
	}
	if paths[len(paths)-1] != "/repos/acme/payments/issues" {
		t.Fatalf("last request = %q, want issue create", paths[len(paths)-1])
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func response(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     make(http.Header),
	}
}
