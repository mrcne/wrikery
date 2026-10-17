package cli

import (
	"context"
	"strings"
	"testing"
)

const commentAnswer = `{"kind":"comments","data":[{"id":"C9","authorId":"U1","text":"Pushed the branch","createdDate":"2026-10-15T12:00:00Z","taskId":"TASK1"}]}`

func TestTaskCommentSendsTheTextAsMe(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env, path, form, _ := recordWrite(t, env, commentAnswer)
	if code := Run(context.Background(), env, []string{"task", "comment", "TASK1", "Pushed", "the", "branch"}); code != exitOK {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if *path != "POST /tasks/TASK1/comments" || form.Get("text") != "Pushed the branch" {
		t.Errorf("request = %q %v, want the comment on the task", *path, *form)
	}
	if !strings.Contains(out.String(), "commented on Headless commands for scripts\n") {
		t.Errorf("out:\n%s", out.String())
	}
	comments, err := env.Store.Comments().ListForTask(context.Background(), "TASK1")
	if err != nil || len(comments) != 1 || comments[0].ID != "C9" || comments[0].AuthorID != "U1" {
		t.Errorf("comments = %+v, %v, want the server's comment in the cache", comments, err)
	}
}

func TestTaskCommentOfflineQueuesALocalCommentAsMe(t *testing.T) {
	env, out, _ := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, nil)
	if code := Run(context.Background(), env, []string{"task", "comment", "headless", "Still", "offline"}); code != exitQueued {
		t.Fatalf("code = %d\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "queued: comment on Headless commands for scripts\n") {
		t.Errorf("out:\n%s", out.String())
	}
	comments, err := env.Store.Comments().ListForTask(context.Background(), "TASK1")
	if err != nil || len(comments) != 1 || comments[0].ID != "local:1" || comments[0].AuthorID != "U1" || comments[0].Text != "Still offline" {
		t.Errorf("comments = %+v, %v, want the local comment as me", comments, err)
	}
}

func TestTaskCommentNeedsATextAndAKnownAuthor(t *testing.T) {
	env, _, errOut := testEnv(t)
	seedBoard(t, env.Store)
	env = withNetwork(t, env, nil)
	if code := Run(context.Background(), env, []string{"task", "comment", "TASK1"}); code != exitUsage {
		t.Errorf("no text code = %d, want %d", code, exitUsage)
	}
	if code := Run(context.Background(), env, []string{"task", "comment", "TASK1", "   "}); code != exitUsage {
		t.Errorf("blank text code = %d, want %d", code, exitUsage)
	}
	if pending, _, _ := env.Store.Outbox().Counts(context.Background()); pending != 0 {
		t.Errorf("a usage error queued a row")
	}
	fresh, _, freshErr := testEnv(t)
	if code := Run(context.Background(), fresh, []string{"task", "comment", "TASK1", "hello"}); code != exitError {
		t.Errorf("no me id code = %d, want %d", code, exitError)
	}
	if !strings.Contains(freshErr.String(), "does not know who you are") {
		t.Errorf("stderr = %q, want the author problem named", freshErr.String())
	}
	_ = errOut
}
