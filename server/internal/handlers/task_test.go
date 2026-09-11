package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/XiovV/calich/server/internal/apptest"
	"github.com/XiovV/calich/server/internal/httpauth"
	"github.com/XiovV/calich/server/internal/service"
)

// taskHandlerTestServer bundles the HTTP surface Tasks needs (#310,
// ADR-0083): Register (to mint real Users, each with their own Workspace
// and auto-provisioned default Inbox Task List) plus the /api/task-lists
// and /api/tasks routes, gated exactly like router.New wires them.
type taskHandlerTestServer struct {
	srv   *httptest.Server
	graph *service.Graph
}

func newTaskHandlerTestServer(t *testing.T) *taskHandlerTestServer {
	t.Helper()

	cfg := apptest.Config(t)
	cfg.InitialName, cfg.InitialEmail, cfg.InitialPassword = "", "", ""
	cfg.EnableSignups = true
	g := newTestGraphWithConfig(t, cfg)

	auth := g.Auth
	taskLists := g.TaskLists
	tasks := g.Tasks

	authHandler := NewAuthHandler(auth, g.RateLimiter, false, false, false, true)
	taskListHandler := NewTaskListHandler(taskLists)
	taskHandler := NewTaskHandler(tasks)

	r := chi.NewRouter()
	r.Post("/api/auth/register", authHandler.Register)

	r.Route("/api/task-lists", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Use(httpauth.RequireEnabledUser(auth))
		r.Use(httpauth.RequireWorkspace(g.Workspaces))

		r.Get("/", taskListHandler.List)
		r.Post("/", taskListHandler.Create)
		r.Patch("/{id}", taskListHandler.Rename)
		r.Patch("/{id}/color", taskListHandler.Recolor)
		r.Put("/{id}/default", taskListHandler.SetDefault)
		r.Delete("/{id}", taskListHandler.Delete)
	})

	r.Route("/api/tasks", func(r chi.Router) {
		r.Use(httpauth.RequireAuth(auth))
		r.Use(httpauth.RequireEnabledUser(auth))
		r.Use(httpauth.RequireWorkspace(g.Workspaces))

		r.Get("/", taskHandler.List)
		r.Get("/completed", taskHandler.ListCompleted)
		r.Post("/", taskHandler.Create)
		r.Patch("/{id}", taskHandler.Update)
		r.Put("/{id}/complete", taskHandler.Complete)
		r.Delete("/{id}/complete", taskHandler.Uncomplete)
		r.Delete("/{id}", taskHandler.Delete)
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &taskHandlerTestServer{srv: srv, graph: g}
}

func (s *taskHandlerTestServer) register(t *testing.T, username string) (accessToken string, userID, workspaceID int64) {
	t.Helper()
	ctx := context.Background()

	body, err := json.Marshal(registerRequest{Name: username, Email: username + "@example.com", Password: "hunter22"})
	if err != nil {
		t.Fatalf("marshal register request: %v", err)
	}
	resp, err := http.Post(s.srv.URL+"/api/auth/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/auth/register: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 registering %s, got %d", username, resp.StatusCode)
	}
	var logged loginResponse
	if err := json.NewDecoder(resp.Body).Decode(&logged); err != nil {
		t.Fatalf("decode register response: %v", err)
	}

	users := s.graph.UserRepo
	user, err := users.GetByEmail(ctx, username+"@example.com")
	if err != nil {
		t.Fatalf("get %s: %v", username, err)
	}
	workspaces, err := s.graph.Workspaces.ListForUser(ctx, user.ID)
	if err != nil || len(workspaces) != 1 {
		t.Fatalf("list workspaces for %s: %v (%d)", username, err, len(workspaces))
	}

	return logged.AccessToken, user.ID, workspaces[0].ID
}

func (s *taskHandlerTestServer) do(t *testing.T, method, path, accessToken string, workspaceID int64, body any) *http.Response {
	t.Helper()

	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, s.srv.URL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Workspace-Id", strconv.FormatInt(workspaceID, 10))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// defaultTaskListID returns the caller's auto-provisioned default (Inbox)
// Task List id.
func (s *taskHandlerTestServer) defaultTaskListID(t *testing.T, accessToken string, workspaceID int64) int64 {
	t.Helper()

	resp := s.do(t, http.MethodGet, "/api/task-lists/", accessToken, workspaceID, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 listing task lists, got %d", resp.StatusCode)
	}
	var lists []taskListResponse
	if err := json.NewDecoder(resp.Body).Decode(&lists); err != nil {
		t.Fatalf("decode list task lists response: %v", err)
	}
	for _, l := range lists {
		if l.IsDefault {
			return l.ID
		}
	}
	t.Fatalf("expected a default task list, got %v", lists)
	return 0
}

func (s *taskHandlerTestServer) createTaskList(t *testing.T, accessToken string, workspaceID int64, name, color string) taskListResponse {
	t.Helper()

	resp := s.do(t, http.MethodPost, "/api/task-lists/", accessToken, workspaceID, createTaskListRequest{Name: name, Color: color})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating task list, got %d", resp.StatusCode)
	}
	var list taskListResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode create task list response: %v", err)
	}
	return list
}

func (s *taskHandlerTestServer) createTask(t *testing.T, accessToken string, workspaceID, taskListID int64, title string) taskResponse {
	t.Helper()

	resp := s.do(t, http.MethodPost, "/api/tasks/", accessToken, workspaceID, createTaskRequest{Title: title, TaskListID: taskListID})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 creating task, got %d", resp.StatusCode)
	}
	var task taskResponse
	if err := json.NewDecoder(resp.Body).Decode(&task); err != nil {
		t.Fatalf("decode create task response: %v", err)
	}
	return task
}

func (s *taskHandlerTestServer) listTasks(t *testing.T, accessToken string, workspaceID int64) []taskResponse {
	t.Helper()

	resp := s.do(t, http.MethodGet, "/api/tasks/", accessToken, workspaceID, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 listing tasks, got %d", resp.StatusCode)
	}
	var tasks []taskResponse
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode list tasks response: %v", err)
	}
	return tasks
}

func (s *taskHandlerTestServer) listCompletedTasks(t *testing.T, accessToken string, workspaceID int64) []taskResponse {
	t.Helper()

	resp := s.do(t, http.MethodGet, "/api/tasks/completed", accessToken, workspaceID, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 listing completed tasks, got %d", resp.StatusCode)
	}
	var tasks []taskResponse
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode list completed tasks response: %v", err)
	}
	return tasks
}

func TestTaskHandler_Create_QuickAdd(t *testing.T) {
	s := newTaskHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")
	inboxID := s.defaultTaskListID(t, token, workspaceID)

	task := s.createTask(t, token, workspaceID, inboxID, "Buy milk")
	if task.Title != "Buy milk" {
		t.Fatalf("expected title %q, got %q", "Buy milk", task.Title)
	}
	if task.TaskListID != inboxID {
		t.Fatalf("expected task list id %d, got %d", inboxID, task.TaskListID)
	}
	if task.Completed {
		t.Fatalf("expected a freshly created task to be incomplete")
	}
}

func TestTaskHandler_Create_EmptyTitleRejected(t *testing.T) {
	s := newTaskHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")
	inboxID := s.defaultTaskListID(t, token, workspaceID)

	resp := s.do(t, http.MethodPost, "/api/tasks/", token, workspaceID, createTaskRequest{Title: "   ", TaskListID: inboxID})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestTaskHandler_Create_RejectsAnotherUsersTaskList(t *testing.T) {
	s := newTaskHandlerTestServer(t)
	aliceToken, _, aliceWorkspaceID := s.register(t, "alice")
	bobToken, _, bobWorkspaceID := s.register(t, "bob")
	bobInboxID := s.defaultTaskListID(t, bobToken, bobWorkspaceID)
	_ = aliceWorkspaceID

	resp := s.do(t, http.MethodPost, "/api/tasks/", aliceToken, aliceWorkspaceID, createTaskRequest{Title: "Hijack", TaskListID: bobInboxID})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 filing a task into another user's task list, got %d", resp.StatusCode)
	}
}

func TestTaskHandler_List_UnwindowedIncludesUndated(t *testing.T) {
	s := newTaskHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")
	inboxID := s.defaultTaskListID(t, token, workspaceID)

	s.createTask(t, token, workspaceID, inboxID, "Buy milk")
	s.createTask(t, token, workspaceID, inboxID, "Walk the dog")

	tasks := s.listTasks(t, token, workspaceID)
	if len(tasks) != 2 {
		t.Fatalf("expected both undated tasks to appear in the unwindowed list, got %v", tasks)
	}
}

// TestTaskHandler_List_ScopedToWorkspace covers the other half of scoping
// TestTaskHandler_SecondUserCannotReadUpdateCompleteOrDeleteFirstsTask
// doesn't: the same User, switching between two of their own Workspaces,
// rather than two different Users.
func TestTaskHandler_List_ScopedToWorkspace(t *testing.T) {
	s := newTaskHandlerTestServer(t)
	token, userID, firstWorkspaceID := s.register(t, "alice")
	firstInboxID := s.defaultTaskListID(t, token, firstWorkspaceID)
	s.createTask(t, token, firstWorkspaceID, firstInboxID, "Buy milk")

	secondWorkspace, err := s.graph.Workspaces.CreateForOwner(context.Background(), userID, "Second workspace")
	if err != nil {
		t.Fatalf("create second workspace: %v", err)
	}

	tasks := s.listTasks(t, token, secondWorkspace.ID)
	if len(tasks) != 0 {
		t.Fatalf("expected no tasks from the first workspace to leak into the second, got %v", tasks)
	}
}

func TestTaskHandler_List_ExcludesCompletedUnlessAsked(t *testing.T) {
	s := newTaskHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")
	inboxID := s.defaultTaskListID(t, token, workspaceID)

	task := s.createTask(t, token, workspaceID, inboxID, "Buy milk")

	completeResp := s.do(t, http.MethodPut, "/api/tasks/"+strconv.FormatInt(task.ID, 10)+"/complete", token, workspaceID, nil)
	defer completeResp.Body.Close()
	if completeResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 completing, got %d", completeResp.StatusCode)
	}

	tasks := s.listTasks(t, token, workspaceID)
	if len(tasks) != 0 {
		t.Fatalf("expected the completed task to be excluded from the default list, got %v", tasks)
	}

	completed := s.listCompletedTasks(t, token, workspaceID)
	if len(completed) != 1 || !completed[0].Completed {
		t.Fatalf("expected the completed task to appear when asked for, got %v", completed)
	}
}

func TestTaskHandler_CompleteAndUncomplete(t *testing.T) {
	s := newTaskHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")
	inboxID := s.defaultTaskListID(t, token, workspaceID)
	task := s.createTask(t, token, workspaceID, inboxID, "Buy milk")

	completeResp := s.do(t, http.MethodPut, "/api/tasks/"+strconv.FormatInt(task.ID, 10)+"/complete", token, workspaceID, nil)
	defer completeResp.Body.Close()
	if completeResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 completing, got %d", completeResp.StatusCode)
	}
	var completed taskResponse
	if err := json.NewDecoder(completeResp.Body).Decode(&completed); err != nil {
		t.Fatalf("decode complete response: %v", err)
	}
	if !completed.Completed {
		t.Fatalf("expected the task to be marked completed")
	}

	uncompleteResp := s.do(t, http.MethodDelete, "/api/tasks/"+strconv.FormatInt(task.ID, 10)+"/complete", token, workspaceID, nil)
	defer uncompleteResp.Body.Close()
	if uncompleteResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 uncompleting, got %d", uncompleteResp.StatusCode)
	}
	var uncompleted taskResponse
	if err := json.NewDecoder(uncompleteResp.Body).Decode(&uncompleted); err != nil {
		t.Fatalf("decode uncomplete response: %v", err)
	}
	if uncompleted.Completed {
		t.Fatalf("expected the task to be marked incomplete again")
	}
}

func TestTaskHandler_Update(t *testing.T) {
	s := newTaskHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")
	inboxID := s.defaultTaskListID(t, token, workspaceID)
	task := s.createTask(t, token, workspaceID, inboxID, "Buy milk")

	resp := s.do(t, http.MethodPatch, "/api/tasks/"+strconv.FormatInt(task.ID, 10), token, workspaceID, updateTaskRequest{Title: "Buy oat milk"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 updating, got %d", resp.StatusCode)
	}
	var updated taskResponse
	if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updated.Title != "Buy oat milk" {
		t.Fatalf("expected title %q, got %q", "Buy oat milk", updated.Title)
	}
}

func TestTaskHandler_Delete(t *testing.T) {
	s := newTaskHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")
	inboxID := s.defaultTaskListID(t, token, workspaceID)
	task := s.createTask(t, token, workspaceID, inboxID, "Buy milk")

	resp := s.do(t, http.MethodDelete, "/api/tasks/"+strconv.FormatInt(task.ID, 10), token, workspaceID, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 deleting, got %d", resp.StatusCode)
	}

	tasks := s.listTasks(t, token, workspaceID)
	if len(tasks) != 0 {
		t.Fatalf("expected the task to be gone, got %v", tasks)
	}
}

// TestTaskHandler_SecondUserCannotReadUpdateCompleteOrDeleteFirstsTask
// proves the privacy boundary between two Users holds for Tasks exactly as
// it does for Task Lists (#310, ADR-0083): bob is even added as a Member of
// alice's Workspace, and it still doesn't matter.
func TestTaskHandler_SecondUserCannotReadUpdateCompleteOrDeleteFirstsTask(t *testing.T) {
	s := newTaskHandlerTestServer(t)
	aliceToken, _, workspaceID := s.register(t, "alice")
	bobToken, bobID, _ := s.register(t, "bob")
	if _, err := s.graph.DB.Exec("INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, ?)", workspaceID, bobID, "member"); err != nil {
		t.Fatalf("add bob to alice's workspace: %v", err)
	}

	inboxID := s.defaultTaskListID(t, aliceToken, workspaceID)
	task := s.createTask(t, aliceToken, workspaceID, inboxID, "Buy milk")

	updateResp := s.do(t, http.MethodPatch, "/api/tasks/"+strconv.FormatInt(task.ID, 10), bobToken, workspaceID, updateTaskRequest{Title: "Hijacked"})
	defer updateResp.Body.Close()
	if updateResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 updating, got %d", updateResp.StatusCode)
	}

	completeResp := s.do(t, http.MethodPut, "/api/tasks/"+strconv.FormatInt(task.ID, 10)+"/complete", bobToken, workspaceID, nil)
	defer completeResp.Body.Close()
	if completeResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 completing, got %d", completeResp.StatusCode)
	}

	deleteResp := s.do(t, http.MethodDelete, "/api/tasks/"+strconv.FormatInt(task.ID, 10), bobToken, workspaceID, nil)
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 deleting, got %d", deleteResp.StatusCode)
	}

	survivors := s.listTasks(t, aliceToken, workspaceID)
	if len(survivors) != 1 || survivors[0].Title != "Buy milk" {
		t.Fatalf("expected alice's task to survive untouched, got %v", survivors)
	}
}

// TestTaskHandler_DeletingTaskListReparentsItsTasksToTheDefault covers the
// other half of #310: deleting a Task List moves its Tasks to the default
// rather than destroying them.
func TestTaskHandler_DeletingTaskListReparentsItsTasksToTheDefault(t *testing.T) {
	s := newTaskHandlerTestServer(t)
	token, _, workspaceID := s.register(t, "alice")
	inboxID := s.defaultTaskListID(t, token, workspaceID)
	work := s.createTaskList(t, token, workspaceID, "Work", "#12809CFF")

	task := s.createTask(t, token, workspaceID, work.ID, "Ship the feature")

	deleteResp := s.do(t, http.MethodDelete, "/api/task-lists/"+strconv.FormatInt(work.ID, 10), token, workspaceID, nil)
	defer deleteResp.Body.Close()
	if deleteResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 deleting the task list, got %d", deleteResp.StatusCode)
	}

	tasks := s.listTasks(t, token, workspaceID)
	if len(tasks) != 1 || tasks[0].ID != task.ID {
		t.Fatalf("expected the task to survive, got %v", tasks)
	}
	if tasks[0].TaskListID != inboxID {
		t.Fatalf("expected the task to be reparented to the default task list %d, got %d", inboxID, tasks[0].TaskListID)
	}
}
