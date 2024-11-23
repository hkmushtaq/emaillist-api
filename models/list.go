package models

import "time"

type List struct {
	ID        string    `json:"id" firestore:"id"`
	PublicID  string    `json:"publicId" firestore:"publicId"`
	Name      string    `json:"name" firestore:"name"`
	CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`
}

type ListSubmission struct {
	ID        string    `json:"id" firestore:"-"`
	ListID    string    `json:"listId" firestore:"listId"`
	Email     string    `json:"email" firestore:"email"`
	CreatedAt time.Time `json:"createdAt" firestore:"createdAt"`
}
