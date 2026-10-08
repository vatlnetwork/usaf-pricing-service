package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"

	"usaf-pricing-service/domain"
	"usaf-pricing-service/internal/config"
)

type MongoDB struct {
	client     *mongo.Client
	collection *mongo.Collection
}

type programDocument struct {
	ID       bson.ObjectID        `bson:"_id"`
	Revision int64                `bson:"_revision"`
	Program  domain.VendorProgram `bson:",inline"`
}

func OpenMongoDB(ctx context.Context, cfg config.MongoDB) (*MongoDB, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(cfg.URI).
		SetConnectTimeout(cfg.ConnectTimeout()).
		SetServerSelectionTimeout(cfg.ConnectTimeout()).
		SetTimeout(cfg.OperationTimeout()).
		SetWriteConcern(writeconcern.Majority()))
	if err != nil {
		return nil, fmt.Errorf("configure MongoDB: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout())
	defer cancel()
	if err := client.Ping(pingCtx, readpref.Primary()); err != nil {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = client.Disconnect(cleanupCtx)
		return nil, fmt.Errorf("connect to MongoDB: %w", err)
	}
	return &MongoDB{client: client, collection: client.Database(cfg.Database).Collection(cfg.Collection)}, nil
}

func (m *MongoDB) Close(ctx context.Context) error {
	return m.client.Disconnect(ctx)
}

func (m *MongoDB) Create(ctx context.Context, program *domain.VendorProgram) error {
	doc := programDocument{ID: bson.NewObjectID(), Revision: 1, Program: *program}
	if _, err := m.collection.InsertOne(ctx, doc); err != nil {
		return err
	}
	program.Id = doc.ID.Hex()
	return nil
}

func (m *MongoDB) getDocument(ctx context.Context, id string) (*programDocument, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, ErrInvalidID
	}
	var doc programDocument
	if err := m.collection.FindOne(ctx, bson.M{"_id": objectID}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	doc.Program.Id = doc.ID.Hex()
	return &doc, nil
}

func (m *MongoDB) Get(ctx context.Context, id string) (*domain.VendorProgram, error) {
	doc, err := m.getDocument(ctx, id)
	if err != nil {
		return nil, err
	}
	return &doc.Program, nil
}

// GetByVendor requires an exact, unambiguous match among unexpired programs.
// Filter before applying the limit so expired programs cannot hide an active
// program or cause a false ambiguity. Null also matches legacy missing fields.
func (m *MongoDB) GetByVendor(ctx context.Context, vendor string) (*domain.VendorProgram, error) {
	filter := bson.M{
		"vendor": vendor,
		"$or": bson.A{
			bson.M{"expires_at": nil},
			bson.M{"expires_at": bson.M{"$gt": time.Now()}},
		},
	}
	cursor, err := m.collection.Find(ctx, filter, options.Find().SetLimit(2).
		SetCollation(&options.Collation{Locale: "simple"}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []programDocument
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, ErrNotFound
	}
	if len(docs) > 1 {
		return nil, ErrAmbiguousVendor
	}
	docs[0].Program.Id = docs[0].ID.Hex()
	return &docs[0].Program, nil
}

func (m *MongoDB) List(ctx context.Context, limit, offset int64) ([]domain.VendorProgram, error) {
	cursor, err := m.collection.Find(ctx, bson.D{}, options.Find().SetLimit(limit).SetSkip(offset).
		SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	programs := make([]domain.VendorProgram, 0)
	for cursor.Next(ctx) {
		var doc programDocument
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}
		doc.Program.Id = doc.ID.Hex()
		programs = append(programs, doc.Program)
	}
	return programs, cursor.Err()
}

func (m *MongoDB) Update(ctx context.Context, id string, mutate func(*domain.VendorProgram) error) (*domain.VendorProgram, error) {
	doc, err := m.getDocument(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := mutate(&doc.Program); err != nil {
		return nil, err
	}
	filter := bson.M{"_id": doc.ID, "_revision": doc.Revision}
	doc.Revision++
	result, err := m.collection.ReplaceOne(ctx, filter, doc)
	if err != nil {
		return nil, err
	}
	if result.MatchedCount == 0 {
		return nil, ErrConflict
	}
	return &doc.Program, nil
}

func (m *MongoDB) Delete(ctx context.Context, id string) error {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return ErrInvalidID
	}
	result, err := m.collection.DeleteOne(ctx, bson.M{"_id": objectID})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}
