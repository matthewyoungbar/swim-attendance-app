package db

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/matthewyoungbar/swim-attendance-app/internal/models"
)

func (c *Client) CreateUser(ctx context.Context, u models.User) error {
	u.PK = "USER#" + u.Email
	u.SK = models.UserSK
	u.RecordType = models.RecordTypeUser
	item, err := attributevalue.MarshalMap(u)
	if err != nil {
		return fmt.Errorf("marshal user: %w", err)
	}
	_, err = c.ddb.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(c.table),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(pk)"),
	})
	if err != nil {
		if isConditionFailed(err) {
			return fmt.Errorf("user_exists")
		}
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

func (c *Client) GetUser(ctx context.Context, email string) (*models.User, error) {
	out, err := c.ddb.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(c.table),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: "USER#" + email},
			"sk": &types.AttributeValueMemberS{Value: models.UserSK},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if out.Item == nil {
		return nil, nil
	}
	var u models.User
	if err := attributevalue.UnmarshalMap(out.Item, &u); err != nil {
		return nil, fmt.Errorf("unmarshal user: %w", err)
	}
	if _, exists := out.Item["isActive"]; !exists {
		u.IsActive = true
	}
	return &u, nil
}

func (c *Client) ListCoaches(ctx context.Context) ([]models.User, error) {
	out, err := c.ddb.Scan(ctx, &dynamodb.ScanInput{
		TableName:        aws.String(c.table),
		FilterExpression: aws.String("sk = :sk AND isCoach = :true"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":sk":   &types.AttributeValueMemberS{Value: models.UserSK},
			":true": &types.AttributeValueMemberBOOL{Value: true},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("scan coaches: %w", err)
	}
	var coaches []models.User
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &coaches); err != nil {
		return nil, fmt.Errorf("unmarshal coaches: %w", err)
	}
	return coaches, nil
}

func (c *Client) ListUsers(ctx context.Context) ([]models.User, error) {
	out, err := c.ddb.Scan(ctx, &dynamodb.ScanInput{
		TableName:        aws.String(c.table),
		FilterExpression: aws.String("sk = :sk"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":sk": &types.AttributeValueMemberS{Value: models.UserSK},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("scan users: %w", err)
	}
	var users []models.User
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &users); err != nil {
		return nil, fmt.Errorf("unmarshal users: %w", err)
	}
	for i, item := range out.Items {
		if _, exists := item["isActive"]; !exists {
			users[i].IsActive = true
		}
	}
	return users, nil
}

func (c *Client) UpdateUserProfile(ctx context.Context, email, firstName, lastName, preferredName, phone string) error {
	_, err := c.ddb.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(c.table),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: "USER#" + email},
			"sk": &types.AttributeValueMemberS{Value: models.UserSK},
		},
		UpdateExpression: aws.String("SET firstName = :fn, lastName = :ln, preferredName = :pn, phone = :ph"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":fn": &types.AttributeValueMemberS{Value: firstName},
			":ln": &types.AttributeValueMemberS{Value: lastName},
			":pn": &types.AttributeValueMemberS{Value: preferredName},
			":ph": &types.AttributeValueMemberS{Value: phone},
		},
	})
	return err
}

func (c *Client) UpdateUserRoles(ctx context.Context, email string, isAdmin, isCoach, isActive bool) error {
	_, err := c.ddb.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(c.table),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: "USER#" + email},
			"sk": &types.AttributeValueMemberS{Value: models.UserSK},
		},
		UpdateExpression:    aws.String("SET isAdmin = :admin, isCoach = :coach, isActive = :active"),
		ConditionExpression: aws.String("attribute_exists(pk)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":admin":  &types.AttributeValueMemberBOOL{Value: isAdmin},
			":coach":  &types.AttributeValueMemberBOOL{Value: isCoach},
			":active": &types.AttributeValueMemberBOOL{Value: isActive},
		},
	})
	if err != nil {
		if isConditionFailed(err) {
			return fmt.Errorf("user_not_found")
		}
		return fmt.Errorf("update user roles: %w", err)
	}
	return nil
}

func (c *Client) SaveWebAuthnSession(ctx context.Context, sessionID string, data []byte) error {
	s := models.WebAuthnSession{
		PK:         "WA_SESSION#" + sessionID,
		SK:         models.WebAuthnSessionSK,
		RecordType: models.RecordTypeWASession,
		Data:       string(data),
		TTL:        time.Now().Add(5 * time.Minute).Unix(),
	}
	item, err := attributevalue.MarshalMap(s)
	if err != nil {
		return err
	}
	_, err = c.ddb.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(c.table),
		Item:      item,
	})
	return err
}

func (c *Client) GetWebAuthnSession(ctx context.Context, sessionID string) ([]byte, error) {
	out, err := c.ddb.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(c.table),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: "WA_SESSION#" + sessionID},
			"sk": &types.AttributeValueMemberS{Value: models.WebAuthnSessionSK},
		},
	})
	if err != nil {
		return nil, err
	}
	if out.Item == nil {
		return nil, fmt.Errorf("session not found")
	}
	var s models.WebAuthnSession
	if err := attributevalue.UnmarshalMap(out.Item, &s); err != nil {
		return nil, err
	}
	return []byte(s.Data), nil
}

func (c *Client) DeleteWebAuthnSession(ctx context.Context, sessionID string) error {
	_, err := c.ddb.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(c.table),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: "WA_SESSION#" + sessionID},
			"sk": &types.AttributeValueMemberS{Value: models.WebAuthnSessionSK},
		},
	})
	return err
}