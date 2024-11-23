package dtos

import (
	"scavngr.io/api/models"
)

type CreateListRequest struct {
	Name string `json:"name"`
}

type GetListResponse struct {
	List models.List `json:"list"`
}

type CreateSubmissionRequest struct {
	Email string `json:"email"`
}

type QueuedSubmission struct {
	PublicListID      string
	SubmissionRequest CreateSubmissionRequest
}
