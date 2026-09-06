package dashboard

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Siddhant-K-code/agent-harness/internal/delivery"
)

func (s *Server) delivery(w http.ResponseWriter, r *http.Request) {
	service := delivery.Service{Root: s.options.Root, DB: s.db, Remote: delivery.GitHub{}}
	id := r.PathValue("id")
	if r.Method == "GET" {
		v, err := service.Status(id)
		if err != nil {
			fail(w, err)
			return
		}
		send(w, v)
		return
	}
	var a struct {
		Action  string `json:"action"`
		Hash    string `json:"approval_hash"`
		Approve bool   `json:"approve"`
	}
	if err := decodeChat(w, r, &a); err != nil {
		fail(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	var result any
	var err error
	switch a.Action {
	case "preview":
		result, err = service.Prepare(ctx, id)
	case "publish":
		if !a.Approve {
			fail(w, errors.New("explicit approval of the displayed patch is required"))
			return
		}
		result, err = service.Publish(ctx, id, a.Hash)
	case "reconcile":
		result, err = service.Reconcile(ctx, id)
	default:
		err = errors.New("action must be preview, publish, or reconcile")
	}
	if err != nil {
		fail(w, err)
		return
	}
	send(w, result)
}
