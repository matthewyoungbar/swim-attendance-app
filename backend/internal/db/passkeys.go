package db

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	walib "github.com/go-webauthn/webauthn/webauthn"
	"github.com/matthewyoungbar/swim-attendance-app/internal/models"
)

func b64key(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func (c *Client) SavePasskey(ctx context.Context, webAuthnID []byte, email string, cred walib.Credential) error {
	credJSON, err := json.Marshal(cred)
	if err != nil {
		return fmt.Errorf("marshal credential: %w", err)
	}
	p := models.Passkey{
		PK:             "PASSKEY#" + b64key(webAuthnID),
		SK:             "PASSKEY#" + b64key(cred.ID),
		UserEmail:      email,
		CredentialJSON: string(credJSON),
		CreatedAt:      time.Now().UTC(),
	}
	item, err := attributevalue.MarshalMap(p)
	if err != nil {
		return fmt.Errorf("marshal passkey: %w", err)
	}
	_, err = c.ddb.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(c.table),
		Item:      item,
	})
	return err
}

func (c *Client) GetPasskeys(ctx context.Context, webAuthnID []byte) ([]models.Passkey, error) {
	out, err := c.ddb.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(c.table),
		KeyConditionExpression: aws.String("pk = :pk AND begins_with(sk, :prefix)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":     &types.AttributeValueMemberS{Value: "PASSKEY#" + b64key(webAuthnID)},
			":prefix": &types.AttributeValueMemberS{Value: "PASSKEY#"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("query passkeys: %w", err)
	}
	var passkeys []models.Passkey
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &passkeys); err != nil {
		return nil, fmt.Errorf("unmarshal passkeys: %w", err)
	}
	return passkeys, nil
}

func (c *Client) DeletePasskey(ctx context.Context, webAuthnID []byte, credID string) error {
	_, err := c.ddb.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(c.table),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: "PASSKEY#" + b64key(webAuthnID)},
			"sk": &types.AttributeValueMemberS{Value: "PASSKEY#" + credID},
		},
	})
	return err
}

func (c *Client) UpdatePasskey(ctx context.Context, webAuthnID []byte, cred walib.Credential) error {
	credJSON, err := json.Marshal(cred)
	if err != nil {
		return fmt.Errorf("marshal credential: %w", err)
	}
	_, err = c.ddb.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(c.table),
		Key: map[string]types.AttributeValue{
			"pk": &types.AttributeValueMemberS{Value: "PASSKEY#" + b64key(webAuthnID)},
			"sk": &types.AttributeValueMemberS{Value: "PASSKEY#" + b64key(cred.ID)},
		},
		UpdateExpression: aws.String("SET credentialJSON = :cred"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":cred": &types.AttributeValueMemberS{Value: string(credJSON)},
		},
	})
	return err
}