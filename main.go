package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/pubsub"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/rs/zerolog/log"
	"scavngr.io/api/dtos"
	"scavngr.io/api/models"
	"scavngr.io/api/services"
)

func main() {
	gcpProject := os.Getenv("GOOGLE_CLOUD_PROJECT")
	pubSubTopic := os.Getenv("PUBSUB_TOPIC")
	pubSubSubscription := os.Getenv("PUBSUB_SUBSCRIPTION")

	ctx := context.Background()
	firestoreClient, err := firestore.NewClient(ctx, gcpProject)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to init firestore client")
	}

	pubSubClient, err := pubsub.NewClient(ctx, gcpProject)
	if err != nil {
		log.Fatal().Err(err).Msg("unable to init firestore client")
	}

	listService, err := services.NewListService(ctx, services.ListServiceOptions{
		PubSubClient:           pubSubClient,
		FirestoreClient:        firestoreClient,
		PubSubTopicname:        pubSubTopic,
		PubSubSubscriptionName: pubSubSubscription,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("unable to init list service")
	}

	listService.Start(ctx)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Post("/list", func(w http.ResponseWriter, r *http.Request) {
		payload := dtos.CreateListRequest{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			render.Status(r, http.StatusBadRequest)
			render.PlainText(w, r, "")
			return
		}

		list, err := listService.Create(r.Context(), payload)
		if err != nil {
			log.Err(err).Msg("unable to create list")
			render.Status(r, http.StatusInternalServerError)
			render.PlainText(w, r, "")
			return
		}

		render.JSON(w, r, list)
	})

	r.Get("/list/{listID}", func(w http.ResponseWriter, r *http.Request) {
		listID := r.PathValue("listID")
		if listID == "" {
			render.Status(r, http.StatusBadRequest)
			render.PlainText(w, r, "")
			return
		}

		list, err := listService.Get(r.Context(), listID)
		if err != nil {
			if errors.Is(err, models.ErrNotFound) {
				render.Status(r, http.StatusNotFound)
				render.PlainText(w, r, "")
				return
			} else {
				render.Status(r, http.StatusInternalServerError)
				render.PlainText(w, r, "")
				return
			}
		}

		render.JSON(w, r, list)
	})

	r.Get("/list/{listID}/submissions", func(w http.ResponseWriter, r *http.Request) {
		listID := r.PathValue("listID")
		if listID == "" {
			render.Status(r, http.StatusBadRequest)
			render.PlainText(w, r, "")
			return
		}

		list, err := listService.GetSubmissions(r.Context(), listID)
		if err != nil {
			if errors.Is(err, models.ErrNotFound) {
				render.Status(r, http.StatusNotFound)
				render.PlainText(w, r, "")
				return
			} else {
				render.Status(r, http.StatusInternalServerError)
				render.PlainText(w, r, "")
				return
			}
		}

		render.JSON(w, r, list)
	})

	r.Post("/submission/{publicListID}", func(w http.ResponseWriter, r *http.Request) {
		publicListID := r.PathValue("publicListID")

		bodyData, err := io.ReadAll(r.Body)
		if err != nil {
			render.Status(r, http.StatusInternalServerError)
			render.PlainText(w, r, "")
			return
		}

		// Validate the payload
		payload := dtos.CreateSubmissionRequest{}
		if err := json.Unmarshal(bodyData, &payload); err != nil || payload.Email == "" {
			render.Status(r, http.StatusBadRequest)
			render.PlainText(w, r, "")
			return
		}

		err = listService.AddSubmission(r.Context(), publicListID, payload)
		if err != nil {
			render.Status(r, http.StatusInternalServerError)
			render.PlainText(w, r, "")
			return
		}

		render.PlainText(w, r, "")
	})

	log.Info().Msg("Server started")

	http.ListenAndServe(":3000", r)
}
