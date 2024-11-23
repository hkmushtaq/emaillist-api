package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/pubsub"
	gonanoid "github.com/matoous/go-nanoid/v2"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"scavngr.io/api/dtos"
	"scavngr.io/api/models"
)

type ListService interface {
	Start(ctx context.Context) error
	Create(ctx context.Context, newListRequest dtos.CreateListRequest) (models.List, error)
	Get(ctx context.Context, listID string) (models.List, error)
	GetSubmissions(ctx context.Context, listID string) ([]models.ListSubmission, error)
	AddSubmission(ctx context.Context, publicListID string, newSubmissionRequest dtos.CreateSubmissionRequest) error
}

type ListServiceOptions struct {
	FirestoreClient        *firestore.Client
	PubSubClient           *pubsub.Client
	PubSubTopicname        string
	PubSubSubscriptionName string
}

type listService struct {
	firestoreClient    *firestore.Client
	pubSubClient       *pubsub.Client
	pubSubTopic        *pubsub.Topic
	pubSubSubscription *pubsub.Subscription
}

func NewListService(ctx context.Context, opts ListServiceOptions) (ListService, error) {
	topic := opts.PubSubClient.Topic(opts.PubSubTopicname)
	exists, err := topic.Exists(ctx)
	if err != nil {
		log.Err(err).Str("topic", opts.PubSubTopicname).Str("subscription", opts.PubSubSubscriptionName).Msg("failed to check if topic exists")
		return nil, fmt.Errorf("failed to check if topic exists")
	}

	if !exists {
		topic, err = opts.PubSubClient.CreateTopic(ctx, opts.PubSubTopicname)
		if err != nil {
			log.Err(err).Str("topic", opts.PubSubTopicname).Str("subscription", opts.PubSubSubscriptionName).Msg("unable to create pub sub topic")
			return nil, fmt.Errorf("failed to create pub sub topic - %w", err)
		}
	}

	subscription := opts.PubSubClient.Subscription(opts.PubSubSubscriptionName)
	exists, err = subscription.Exists(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check if subscription exists - %w", err)
	}

	if !exists {
		subscription, err = opts.PubSubClient.CreateSubscription(ctx, opts.PubSubSubscriptionName, pubsub.SubscriptionConfig{
			Topic:                     topic,
			EnableExactlyOnceDelivery: true,
		})
		if err != nil {
			log.Err(err).Str("topic", opts.PubSubTopicname).Str("subscription", opts.PubSubSubscriptionName).Msg("unable to create pub sub subscription")
			return nil, fmt.Errorf("failed to create pub sub subscription - %w", err)
		}
	}

	subscription.ReceiveSettings = pubsub.ReceiveSettings{
		MaxExtension:           60 * time.Minute,
		MaxExtensionPeriod:     0,
		MinExtensionPeriod:     0,
		MaxOutstandingMessages: 1000,
		MaxOutstandingBytes:    1e9, // 1G
		NumGoroutines:          10,
	}

	return &listService{
		firestoreClient:    opts.FirestoreClient,
		pubSubClient:       opts.PubSubClient,
		pubSubTopic:        topic,
		pubSubSubscription: subscription,
	}, nil
}

func (s *listService) Start(ctx context.Context) error {
	go func() {
		err := s.pubSubSubscription.Receive(ctx, func(ctx context.Context, m *pubsub.Message) {
			err := s.processQueuedSubmission(ctx, m)
			if err != nil {
				log.Err(err).Msg("error processing queue submission")
				m.Nack()
			} else {
				m.Ack()
			}
		})
		if err != nil {
			log.Err(err).Msg("unable to listen to topic")
		}
	}()

	return nil
}

func (s *listService) Create(ctx context.Context, newListRequest dtos.CreateListRequest) (models.List, error) {
	newList := models.List{
		Name:      newListRequest.Name,
		CreatedAt: time.Now(),
	}

	docID, err := gonanoid.New(21)
	if err != nil {
		return newList, fmt.Errorf("unable to docID - %w", err)
	}

	publicID, err := gonanoid.New(20)
	if err != nil {
		return newList, fmt.Errorf("unable to generate publicID - %w", err)
	}

	newList.ID = docID
	newList.PublicID = publicID

	_, err = s.firestoreClient.Collection("list").Doc(docID).Set(ctx, newList)
	if err != nil {
		return newList, fmt.Errorf("unable to create new list - %w", err)
	}

	return newList, nil
}

func (s *listService) Get(ctx context.Context, listID string) (models.List, error) {
	newList := models.List{}

	docSnapshot, err := s.firestoreClient.Collection("list").Doc(listID).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return newList, models.ErrNotFound
		}

		return newList, fmt.Errorf("unable to find list - %w", err)
	}

	err = docSnapshot.DataTo(&newList)
	if err != nil {
		return newList, fmt.Errorf("unable to marshal from firestore to struct")
	}

	return newList, nil
}

func (s *listService) GetSubmissions(ctx context.Context, listID string) ([]models.ListSubmission, error) {
	submissions := []models.ListSubmission{}

	submissionDocSnapshots, err := s.firestoreClient.Collection("list").Doc(listID).Collection("submissions").Documents(ctx).GetAll()
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return submissions, models.ErrNotFound
		}

		return submissions, fmt.Errorf("unable to find list - %w", err)
	}

	for _, submissionDocSnapshot := range submissionDocSnapshots {
		s := models.ListSubmission{}
		err = submissionDocSnapshot.DataTo(&s)
		if err != nil {
			log.Err(err).Msg("unable to unmarshal submission...skipping submission")
			continue
		}
		submissions = append(submissions, s)
	}

	return submissions, nil
}

func (s *listService) AddSubmission(ctx context.Context, publicListID string, newSubmissionRequest dtos.CreateSubmissionRequest) error {
	submission := dtos.QueuedSubmission{
		PublicListID:      publicListID,
		SubmissionRequest: newSubmissionRequest,
	}

	data, err := json.Marshal(submission)
	if err != nil {
		return fmt.Errorf("unable to marshal queued submission - %w", err)
	}

	_, err = s.pubSubTopic.Publish(ctx, &pubsub.Message{
		Data: data,
	}).Get(ctx)

	if err != nil {
		return fmt.Errorf("unable to publish msg - %w", err)
	}

	log.Info().Str("publicListID", publicListID).Msg("queued submission")

	return nil
}

func (s *listService) processQueuedSubmission(ctx context.Context, msg *pubsub.Message) error {
	queuedSubmission := dtos.QueuedSubmission{}
	err := json.Unmarshal(msg.Data, &queuedSubmission)
	if err != nil {
		log.Err(err).Msg("unable to unmarshal event...skipping")
		return nil
	}

	err = s.addSubmission(ctx, queuedSubmission.PublicListID, queuedSubmission.SubmissionRequest)
	if err != nil {
		return err
	}

	return nil
}

func (s *listService) addSubmission(ctx context.Context, publicListID string, newSubmissionRequest dtos.CreateSubmissionRequest) error {
	err := s.firestoreClient.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		docSnapshots, err := s.firestoreClient.Collection("list").Where("publicId", "==", publicListID).Limit(1).Documents(ctx).GetAll()
		if err != nil {
			return fmt.Errorf("there was an error looking up the list")
		}

		if len(docSnapshots) == 0 {
			log.Error().Str("publicId", publicListID).Msg("unable to find list with that id")
			return nil
		}

		listDoc := docSnapshots[0]

		// Check for existing submission
		submissionDocSnapshots, err := listDoc.Ref.Collection("submissions").Where("email", "==", strings.ToLower(newSubmissionRequest.Email)).Limit(1).Documents(ctx).GetAll()
		if err != nil {
			return fmt.Errorf("there was an error checking for existing entry")
		}

		if len(submissionDocSnapshots) > 0 {
			log.Info().Str("publicId", publicListID).Msg("entry exists..skipping")
			return nil
		}

		// Add submission entry
		submission := models.ListSubmission{
			ListID:    listDoc.Ref.ID,
			Email:     strings.ToLower(newSubmissionRequest.Email),
			CreatedAt: time.Now(),
		}
		submissionRef, _, err := listDoc.Ref.Collection("submissions").Add(ctx, submission)
		if err != nil {
			return fmt.Errorf("unable to insert submission for list")
		}

		submission.ID = submissionRef.ID

		return nil
	})
	if err != nil {
		log.Err(err).Str("publicId", publicListID).Msg("unable to insert submission")
		return err
	}

	return nil
}
