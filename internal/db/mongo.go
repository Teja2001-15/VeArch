package db

import (
	"context"
	"fmt"
	"log"
	"time"

	"arch_erp/internal/config"
	"arch_erp/internal/models"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Client is the shared MongoDB client used throughout the application.
var Client *mongo.Client

// Database is the application's primary MongoDB database handle.
var Database *mongo.Database

// Collections provides named handles to each collection.
var Collections struct {
	Users    *mongo.Collection
	Projects *mongo.Collection
	Tasks    *mongo.Collection
}

// Connect establishes a connection to MongoDB and validates it with a ping.
func Connect(cfg *config.Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	clientOpts := options.Client().ApplyURI(cfg.MongoURI)
	client, err := mongo.Connect(clientOpts)
	if err != nil {
		return fmt.Errorf("connecting to MongoDB: %w", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		return fmt.Errorf("pinging MongoDB: %w", err)
	}

	Client = client
	Database = client.Database(cfg.MongoDB)
	Collections.Users = Database.Collection("users")
	Collections.Projects = Database.Collection("projects")
	Collections.Tasks = Database.Collection("tasks")

	log.Println("✓ MongoDB connected:", cfg.MongoURI)
	return nil
}

// EnsureIndexes creates all required unique and compound indexes.
func EnsureIndexes(ctx context.Context) error {
	// users.user_id — unique login handle
	_, err := Collections.Users.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "user_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return fmt.Errorf("user_id index: %w", err)
	}

	// tasks.project_id — fast Kanban lookups per project
	_, err = Collections.Tasks.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "project_id", Value: 1}, {Key: "status", Value: 1}},
	})
	if err != nil {
		return fmt.Errorf("tasks project_id+status index: %w", err)
	}

	// projects.assigned_users — find all projects for a user quickly
	_, err = Collections.Projects.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "assigned_users", Value: 1}},
	})
	if err != nil {
		return fmt.Errorf("projects assigned_users index: %w", err)
	}

	log.Println("✓ MongoDB indexes ensured")
	return nil
}

// SeedAdmin creates the default admin user if no users exist in the database.
// passwordHash must already be a bcrypt hash (hash it in the caller using auth.HashPassword).
func SeedAdmin(ctx context.Context, cfg *config.Config, passwordHash string) error {
	count, err := Collections.Users.CountDocuments(ctx, bson.D{})
	if err != nil {
		return fmt.Errorf("counting users: %w", err)
	}
	if count > 0 {
		return nil // Already seeded
	}

	admin := models.User{
		UserID:       cfg.AdminUserID,
		PasswordHash: passwordHash,
		Name:         cfg.AdminName,
		Email:        cfg.AdminEmail,
		Role:         models.RoleAdmin,
		TokenVersion: 0,
		CreatedAt:    time.Now(),
	}

	_, err = Collections.Users.InsertOne(ctx, admin)
	if err != nil {
		return fmt.Errorf("inserting admin user: %w", err)
	}

	log.Printf("✓ Admin user seeded (user_id: %s)", cfg.AdminUserID)
	return nil
}

// Disconnect cleanly closes the MongoDB client.
func Disconnect(ctx context.Context) {
	if Client != nil {
		if err := Client.Disconnect(ctx); err != nil {
			log.Println("warn: MongoDB disconnect error:", err)
		}
	}
}
