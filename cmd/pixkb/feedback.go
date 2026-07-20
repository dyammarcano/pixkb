package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// feedbackServer collects thumbs-up/down feedback on ask answers into an
// append-only JSONL log, so answer quality can be reviewed and analysed later.
// Mounted only by `serve --ask`.
type feedbackServer struct {
	path string
	mu   sync.Mutex
}

// feedbackEntry is one captured Q&A judgement. Verdict is "up" | "down".
type feedbackEntry struct {
	Question  string        `json:"question"`
	Answer    string        `json:"answer"`
	Refused   bool          `json:"refused"`
	Citations []askCitation `json:"citations,omitempty"`
	Verdict   string        `json:"verdict"`
	Note      string        `json:"note,omitempty"`
	TS        string        `json:"ts"`
}

func (s *feedbackServer) handle(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodPost:
		s.append(w, req)
	case http.MethodGet:
		s.list(w)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *feedbackServer) append(w http.ResponseWriter, req *http.Request) {
	var e feedbackEntry
	if err := json.NewDecoder(req.Body).Decode(&e); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(e.Question) == "" {
		http.Error(w, "missing question", http.StatusBadRequest)
		return
	}
	if e.Verdict != "up" && e.Verdict != "down" {
		http.Error(w, "verdict must be up or down", http.StatusBadRequest)
		return
	}
	e.TS = time.Now().UTC().Format(time.RFC3339)

	line, err := json.Marshal(e)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(append(line, '\n')); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *feedbackServer) list(w http.ResponseWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := readFeedback(s.path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, entries)
}

// readFeedback parses the JSONL log into entries. A missing file is an empty
// log, not an error. Malformed lines are skipped so one bad line never hides the
// rest.
func readFeedback(path string) ([]feedbackEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []feedbackEntry{}, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()
	out := []feedbackEntry{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e feedbackEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		out = append(out, e)
	}
	return out, sc.Err()
}
