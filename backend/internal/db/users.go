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
	return &u, nil
}

func (c *Client) UpdateUserCredentials(ctx context.Context, email, credentialsJSON string) error {
	_, err := c.ddb.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(c.table),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: "USER#" + email},
			"sk": &types.AttributeValueMemberS{Value: models.UserSK},
		},
		UpdateExpression: aws.String("SET credentials = :creds"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":creds": &types.AttributeValueMemberS{Value: credentialsJSON},
		},
	})
	return err
}

func (c *Client) SaveWebAuthnSession(ctx context.Context, sessionID string, data []byte) error {
	s := models.WebAuthnSession{
		PK:   "WA_SESSION#" + sessionID,
		SK:   models.WebAuthnSessionSK,
		Data: string(data),
		TTL:  time.Now().Add(5 * time.Minute).Unix(),
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