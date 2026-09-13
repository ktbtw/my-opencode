package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"time"

	"relay-server/internal/model"
)

type artifactStreamEvent struct {
	kind   string
	chunk  *model.ArtifactChunkPayload
	done   *model.ArtifactDonePayload
	failed *model.ArtifactFailedPayload
}

func (a *App) registerArtifactStream(requestID string) chan artifactStreamEvent {
	ch := make(chan artifactStreamEvent, 32)
	a.artifactMu.Lock()
	a.artifactSubs[requestID] = ch
	a.artifactMu.Unlock()
	return ch
}

func (a *App) unregisterArtifactStream(requestID string) {
	a.artifactMu.Lock()
	delete(a.artifactSubs, requestID)
	a.artifactMu.Unlock()
}

func (a *App) pushArtifactStream(requestID string, evt artifactStreamEvent) {
	if requestID == "" {
		return
	}
	a.artifactMu.Lock()
	ch := a.artifactSubs[requestID]
	a.artifactMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- evt:
	default:
	}
}

func (a *App) streamArtifact(ctx context.Context, task *model.Task, artifact model.Artifact, w io.Writer) error {
	if task == nil {
		return fmt.Errorf("task missing")
	}
	if artifact.ID == "" || artifact.RelativePath == "" {
		return fmt.Errorf("artifact metadata invalid")
	}
	requestID := fmt.Sprintf("artifact_%s_%d", artifact.ID, time.Now().UnixNano())
	ch := a.registerArtifactStream(requestID)
	defer a.unregisterArtifactStream(requestID)

	if err := a.broker.DispatchForOperator(ctx, task.OperatorID, task.AgentID, model.Envelope{
		Type:      "artifact.fetch",
		RequestID: requestID,
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload: model.ArtifactFetchPayload{
			TaskID:       task.ID,
			ArtifactID:   artifact.ID,
			RelativePath: artifact.RelativePath,
		},
	}); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case evt, ok := <-ch:
			if !ok {
				return fmt.Errorf("artifact stream closed")
			}
			switch evt.kind {
			case "chunk":
				if evt.chunk == nil || evt.chunk.Data == "" {
					continue
				}
				buf, err := base64.StdEncoding.DecodeString(evt.chunk.Data)
				if err != nil {
					return err
				}
				if _, err := w.Write(buf); err != nil {
					return err
				}
			case "done":
				return nil
			case "failed":
				if evt.failed != nil && evt.failed.Error != "" {
					return fmt.Errorf("%s", evt.failed.Error)
				}
				return fmt.Errorf("artifact fetch failed")
			}
		}
	}
}
