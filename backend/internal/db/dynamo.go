package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/matthewyoungbar/swim-attendance-app/internal/models"
)

type Client struct {
	ddb   *dynamodb.Client
	table string
}

func NewClient(ctx context.Context) (*Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}
	table := os.Getenv("TABLE_NAME")
	if table == "" {
		table = "swim-app"
	}
	return &Client{ddb: dynamodb.NewFromConfig(cfg), table: table}, nil
}

// ─── Practices ────────────────────────────────────────────────────────────────

// UpsertPractice writes or updates a practice (used when syncing from Google Calendar).
func (c *Client) UpsertPractice(ctx context.Context, p models.Practice) error {
	p.PK = p.ID
	p.SK = models.PracticeSK
	p.RecordType = models.RecordTypePractice
	item, err := attributevalue.MarshalMap(p)
	if err != nil {
		return fmt.Errorf("marshal practice: %w", err)
	}
	_, err = c.ddb.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(c.table),
		Item:      item,
	})
	return err
}

// GetPractices returns upcoming practices (startTime >= now).
func (c *Client) GetPractices(ctx context.Context) ([]models.Practice, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	out, err := c.ddb.Scan(ctx, &dynamodb.ScanInput{
		TableName:        aws.String(c.table),
		FilterExpression: aws.String("sk = :sk AND #st >= :now"),
		ExpressionAttributeNames: map[string]string{
			"#st": "startTime",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":sk":  &types.AttributeValueMemberS{Value: models.PracticeSK},
			":now": &types.AttributeValueMemberS{Value: now},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("scan practices: %w", err)
	}

	var practices []models.Practice
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &practices); err != nil {
		return nil, fmt.Errorf("unmarshal practices: %w", err)
	}
	return practices, nil
}

// GetPractice returns a single practice by ID.
func (c *Client) GetPractice(ctx context.Context, id string) (*models.Practice, error) {
	out, err := c.ddb.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(c.table),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: id},
			"sk": &types.AttributeValueMemberS{Value: models.PracticeSK},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get practice: %w", err)
	}
	if out.Item == nil {
		return nil, nil
	}
	var p models.Practice
	if err := attributevalue.UnmarshalMap(out.Item, &p); err != nil {
		return nil, fmt.Errorf("unmarshal practice: %w", err)
	}
	return &p, nil
}

// IncrementSignupCount atomically increments or decrements the signup counter.
func (c *Client) IncrementSignupCount(ctx context.Context, practiceID string, delta int) error {
	_, err := c.ddb.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(c.table),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: practiceID},
			"sk": &types.AttributeValueMemberS{Value: models.PracticeSK},
		},
		UpdateExpression: aws.String("SET signupCount = if_not_exists(signupCount, :zero) + :delta"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":delta": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", delta)},
			":zero":  &types.AttributeValueMemberN{Value: "0"},
		},
	})
	return err
}

// ─── Signups ──────────────────────────────────────────────────────────────────

// CreateSignup registers a swimmer for a practice.
// Returns an error if they're already signed up.
func (c *Client) CreateSignup(ctx context.Context, s models.Signup) error {
	s.PK = s.PracticeID
	s.SK = s.SwimmerEmail
	s.RecordType = models.RecordTypeSignup
	item, err := attributevalue.MarshalMap(s)
	if err != nil {
		return fmt.Errorf("marshal signup: %w", err)
	}
	_, err = c.ddb.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(c.table),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(pk)"),
	})
	if err != nil {
		if isConditionFailed(err) {
			return fmt.Errorf("already_signed_up")
		}
		return fmt.Errorf("create signup: %w", err)
	}
	return nil
}

// DeleteSignup removes a swimmer's registration.
func (c *Client) DeleteSignup(ctx context.Context, practiceID, swimmerEmail string) error {
	_, err := c.ddb.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(c.table),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: practiceID},
			"sk": &types.AttributeValueMemberS{Value: swimmerEmail},
		},
	})
	return err
}

// GetSignup checks if a specific swimmer is signed up for a practice.
func (c *Client) GetSignup(ctx context.Context, practiceID, swimmerEmail string) (*models.Signup, error) {
	out, err := c.ddb.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(c.table),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: practiceID},
			"sk": &types.AttributeValueMemberS{Value: swimmerEmail},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get signup: %w", err)
	}
	if out.Item == nil {
		return nil, nil
	}
	var s models.Signup
	if err := attributevalue.UnmarshalMap(out.Item, &s); err != nil {
		return nil, fmt.Errorf("unmarshal signup: %w", err)
	}
	return &s, nil
}

// GetSignupsForPractice returns all signups for a given practice.
func (c *Client) GetSignupsForPractice(ctx context.Context, practiceID string) ([]models.Signup, error) {
	out, err := c.ddb.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(c.table),
		KeyConditionExpression: aws.String("pk = :pk"),
		FilterExpression:       aws.String("sk <> :practiceSK"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":        &types.AttributeValueMemberS{Value: practiceID},
			":practiceSK": &types.AttributeValueMemberS{Value: models.PracticeSK},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("query signups: %w", err)
	}
	var signups []models.Signup
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &signups); err != nil {
		return nil, fmt.Errorf("unmarshal signups: %w", err)
	}
	return signups, nil
}

// GetSignupsForSwimmer returns all upcoming practices a swimmer is signed up for.
func (c *Client) GetSignupsForSwimmer(ctx context.Context, swimmerEmail string) ([]models.Signup, error) {
	out, err := c.ddb.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(c.table),
		IndexName:              aws.String("swimmerEmail-index"),
		KeyConditionExpression: aws.String("swimmerEmail = :email"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":email": &types.AttributeValueMemberS{Value: swimmerEmail},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("query swimmer signups: %w", err)
	}
	var signups []models.Signup
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &signups); err != nil {
		return nil, fmt.Errorf("unmarshal signups: %w", err)
	}
	return signups, nil
}

func isConditionFailed(err error) bool {
	var condErr *types.ConditionalCheckFailedException
	return errors.As(err, &condErr)
}