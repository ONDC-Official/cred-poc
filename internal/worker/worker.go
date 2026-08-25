package worker

import (
	"context"
	"log"
	"time"

	"credential-service/internal/models"
	"credential-service/internal/repository"
	"credential-service/internal/service"

	"github.com/google/uuid"
)

type CredentialWorker struct {
	credService *service.CredentialService
	credReqRepo *repository.CredentialRequestRepository
	enumCache   *models.EnumCache
	jobCh       <-chan uuid.UUID
	pollTicker  *time.Ticker
	stopCh      chan struct{}
}

func NewCredentialWorker(
	credService *service.CredentialService,
	credReqRepo *repository.CredentialRequestRepository,
	enumCache *models.EnumCache,
) *CredentialWorker {
	return &CredentialWorker{
		credService: credService,
		credReqRepo: credReqRepo,
		enumCache:   enumCache,
		jobCh:       credService.JobChannel(),
		pollTicker:  time.NewTicker(30 * time.Second),
		stopCh:      make(chan struct{}),
	}
}

func (w *CredentialWorker) Start(ctx context.Context) {
	log.Println("credential worker started")
	go w.run(ctx)
}

func (w *CredentialWorker) Stop() {
	close(w.stopCh)
	w.pollTicker.Stop()
	log.Println("credential worker stopped")
}

func (w *CredentialWorker) run(ctx context.Context) {
	for {
		select {
		case <-w.stopCh:
			return
		case <-ctx.Done():
			return
		case id := <-w.jobCh:
			w.processOne(ctx, id)
		case <-w.pollTicker.C:
			w.pollPending(ctx)
		}
	}
}

func (w *CredentialWorker) processOne(ctx context.Context, id uuid.UUID) {
	log.Printf("processing credential request %s", id)
	if err := w.credService.ProcessCredentialRequest(ctx, id); err != nil {
		log.Printf("error processing credential request %s: %v", id, err)
	}
}

func (w *CredentialWorker) pollPending(ctx context.Context) {
	pendingID := w.enumCache.VerificationStatusID(models.VerificationPending)
	requests, err := w.credReqRepo.FindPending(pendingID, 50)
	if err != nil {
		log.Printf("error polling pending requests: %v", err)
		return
	}

	for _, req := range requests {
		w.processOne(ctx, req.ID)
	}
}
