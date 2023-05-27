package email

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/harryzcy/mailbox/internal/env"
	"github.com/harryzcy/mailbox/internal/util/htmlutil"
	"github.com/harryzcy/mailbox/internal/util/mockutil"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

var (
	errSend       = errors.New("test send error")
	errBatchWrite = errors.New("test batch write error")
)

type mockSaveEmailAPI struct {
	mockGetItem           mockGetItemAPI
	mockPutItem           func(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
	mockTransactWriteItem mockutil.MockTransactWriteItemAPI
	mockSendEmail         func(ctx context.Context, params *sesv2.SendEmailInput, optFns ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error)
}

func (m mockSaveEmailAPI) GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	return m.mockGetItem(ctx, params, optFns...)
}

func (m mockSaveEmailAPI) PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	return m.mockPutItem(ctx, params, optFns...)
}

func (m mockSaveEmailAPI) TransactWriteItems(ctx context.Context, params *dynamodb.TransactWriteItemsInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error) {
	return m.mockTransactWriteItem(ctx, params, optFns...)
}

func (m mockSaveEmailAPI) SendEmail(ctx context.Context, params *sesv2.SendEmailInput, optFns ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error) {
	return m.mockSendEmail(ctx, params, optFns...)
}

func TestTetUpdatedTime(t *testing.T) {
	assert.NotNil(t, getUpdatedTime())
}

func TestSave(t *testing.T) {
	oldGetUpdatedTime := getUpdatedTime
	getUpdatedTime = func() time.Time { return time.Date(2022, 3, 16, 16, 55, 45, 0, time.UTC) }
	defer func() { getUpdatedTime = oldGetUpdatedTime }()

	env.TableName = "table-for-save"
	tests := []struct {
		client       func(t *testing.T) SaveAndSendEmailAPI
		input        SaveInput
		generateText func(html string) (string, error)
		expected     *SaveResult
		expectedErr  error
	}{
		{ // without Send
			client: func(t *testing.T) SaveAndSendEmailAPI {
				return mockSaveEmailAPI{
					mockGetItem: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
						return &dynamodb.GetItemOutput{
							Item: map[string]types.AttributeValue{},
						}, nil
					},
					mockPutItem: func(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
						t.Helper()

						assert.Equal(t, env.TableName, *params.TableName)

						messageID := params.Item["MessageID"].(*types.AttributeValueMemberS).Value
						assert.Equal(t, "draft-example", messageID)

						assert.Equal(t, "MessageID = :messageID", *params.ConditionExpression)
						assert.Contains(t, params.ExpressionAttributeValues, ":messageID")
						assert.Equal(t, "draft-example",
							params.ExpressionAttributeValues[":messageID"].(*types.AttributeValueMemberS).Value,
						)

						return &dynamodb.PutItemOutput{}, nil
					},
				}
			},
			input: SaveInput{
				EmailInput: EmailInput{
					MessageID: "draft-example",
					Subject:   "subject",
					From:      []string{"example@example.com"},
					To:        []string{"example@example.com"},
					Cc:        []string{"example@example.com"},
					Bcc:       []string{"example@example.com"},
					ReplyTo:   []string{"example@example.com"},
					Text:      "text",
					HTML:      "<p>html</p>",
				},
				GenerateText: "off",
			},
			expected: &SaveResult{
				TimeIndex: TimeIndex{
					MessageID:   "draft-example",
					Type:        EmailTypeDraft,
					TimeUpdated: "2022-03-16T16:55:45Z",
				},
				Subject: "subject",
				From:    []string{"example@example.com"},
				To:      []string{"example@example.com"},
				Cc:      []string{"example@example.com"},
				Bcc:     []string{"example@example.com"},
				ReplyTo: []string{"example@example.com"},
				Text:    "text",
				HTML:    "<p>html</p>",
			},
		},
		{ // without Send
			client: func(t *testing.T) SaveAndSendEmailAPI {
				return mockSaveEmailAPI{
					mockGetItem: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
						return &dynamodb.GetItemOutput{
							Item: map[string]types.AttributeValue{},
						}, nil
					},
					mockPutItem: func(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
						return &dynamodb.PutItemOutput{}, nil
					},
				}
			},
			input: SaveInput{
				EmailInput: EmailInput{
					MessageID: "draft-example",
					Subject:   "subject",
					From:      []string{"example@example.com"},
					To:        []string{"example@example.com"},
					Cc:        []string{"example@example.com"},
					Bcc:       []string{"example@example.com"},
					ReplyTo:   []string{"example@example.com"},
					Text:      "text",
					HTML:      "<p>html</p>",
				},
				GenerateText: "on",
			},
			expected: &SaveResult{
				TimeIndex: TimeIndex{
					MessageID:   "draft-example",
					Type:        EmailTypeDraft,
					TimeUpdated: "2022-03-16T16:55:45Z",
				},
				Subject: "subject",
				From:    []string{"example@example.com"},
				To:      []string{"example@example.com"},
				Cc:      []string{"example@example.com"},
				Bcc:     []string{"example@example.com"},
				ReplyTo: []string{"example@example.com"},
				Text:    "html",
				HTML:    "<p>html</p>",
			},
		},
		{ // without Send
			client: func(t *testing.T) SaveAndSendEmailAPI {
				return mockSaveEmailAPI{
					mockGetItem: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
						return &dynamodb.GetItemOutput{
							Item: map[string]types.AttributeValue{},
						}, nil
					},
					mockPutItem: func(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
						return &dynamodb.PutItemOutput{}, nil
					},
				}
			},
			input: SaveInput{
				EmailInput: EmailInput{
					MessageID: "draft-example",
					Subject:   "subject",
					From:      []string{"example@example.com"},
					To:        []string{"example@example.com"},
					Cc:        []string{"example@example.com"},
					Bcc:       []string{"example@example.com"},
					ReplyTo:   []string{"example@example.com"},
					HTML:      "<p>html</p>",
				},
				GenerateText: "auto",
			},
			expected: &SaveResult{
				TimeIndex: TimeIndex{
					MessageID:   "draft-example",
					Type:        EmailTypeDraft,
					TimeUpdated: "2022-03-16T16:55:45Z",
				},
				Subject: "subject",
				From:    []string{"example@example.com"},
				To:      []string{"example@example.com"},
				Cc:      []string{"example@example.com"},
				Bcc:     []string{"example@example.com"},
				ReplyTo: []string{"example@example.com"},
				Text:    "html",
				HTML:    "<p>html</p>",
			},
		},
		{ // with Send
			client: func(t *testing.T) SaveAndSendEmailAPI {
				return mockSaveEmailAPI{
					mockGetItem: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
						return &dynamodb.GetItemOutput{
							Item: map[string]types.AttributeValue{},
						}, nil
					},
					mockPutItem: func(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
						return &dynamodb.PutItemOutput{}, nil
					},
					mockSendEmail: func(ctx context.Context, params *sesv2.SendEmailInput, optFns ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error) {
						return &sesv2.SendEmailOutput{
							MessageId: aws.String("sent-message-id"),
						}, nil
					},
					mockTransactWriteItem: func(ctx context.Context, params *dynamodb.TransactWriteItemsInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error) {
						t.Helper()

						assert.Len(t, params.TransactItems, 2)

						for _, item := range params.TransactItems {
							if item.Delete != nil {
								messageID := item.Delete.Key["MessageID"].(*types.AttributeValueMemberS).Value
								assert.Equal(t, "draft-example", messageID)
							} else if item.Put != nil {
								newMessageID := item.Put.Item["MessageID"].(*types.AttributeValueMemberS).Value
								assert.Equal(t, "sent-message-id", newMessageID)
							} else {
								t.Fatal("unexpected transact item")
							}
						}

						return &dynamodb.TransactWriteItemsOutput{}, nil
					},
				}
			},
			input: SaveInput{
				EmailInput: EmailInput{
					MessageID: "draft-example",
					Subject:   "subject",
					From:      []string{"example@example.com"},
					To:        []string{"example@example.com"},
					Cc:        []string{"example@example.com"},
					Bcc:       []string{"example@example.com"},
					ReplyTo:   []string{"example@example.com"},
					HTML:      "<p>html</p>",
				},
				GenerateText: "auto",
				Send:         true,
			},
			expected: &SaveResult{
				TimeIndex: TimeIndex{
					MessageID:   "sent-message-id",
					Type:        EmailTypeSent,
					TimeUpdated: "2022-03-16T16:55:45Z",
				},
				Subject: "subject",
				From:    []string{"example@example.com"},
				To:      []string{"example@example.com"},
				Cc:      []string{"example@example.com"},
				Bcc:     []string{"example@example.com"},
				ReplyTo: []string{"example@example.com"},
				Text:    "html",
				HTML:    "<p>html</p>",
			},
		},
		{ // without Send
			client: func(t *testing.T) SaveAndSendEmailAPI {
				return mockSaveEmailAPI{
					mockGetItem: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
						return &dynamodb.GetItemOutput{
							Item: map[string]types.AttributeValue{},
						}, nil
					},
					mockPutItem: func(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
						return &dynamodb.PutItemOutput{}, nil
					},
				}
			},
			input: SaveInput{
				EmailInput: EmailInput{
					MessageID: "draft-example",
				},
				GenerateText: "on",
			},
			generateText: func(html string) (string, error) {
				return "", ErrInvalidInput
			},
			expectedErr: ErrInvalidInput,
		},
		{ // without Send
			client: func(t *testing.T) SaveAndSendEmailAPI {
				return mockSaveEmailAPI{
					mockGetItem: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
						return &dynamodb.GetItemOutput{
							Item: map[string]types.AttributeValue{},
						}, nil
					},
					mockPutItem: func(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
						return &dynamodb.PutItemOutput{}, ErrInvalidInput
					},
				}
			},
			input: SaveInput{
				EmailInput: EmailInput{
					MessageID: "draft-example",
				},
			},
			expectedErr: ErrInvalidInput,
		},
		{ // without Send
			client: func(t *testing.T) SaveAndSendEmailAPI {
				return mockSaveEmailAPI{
					mockGetItem: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
						return &dynamodb.GetItemOutput{
							Item: map[string]types.AttributeValue{},
						}, nil
					},
					mockPutItem: func(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
						t.Helper()
						t.Error("this mock shouldn't be reached")
						return &dynamodb.PutItemOutput{}, nil
					},
				}
			},
			expectedErr: ErrEmailIsNotDraft,
		},
		{ // without Send
			client: func(t *testing.T) SaveAndSendEmailAPI {
				return mockSaveEmailAPI{
					mockGetItem: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
						return &dynamodb.GetItemOutput{
							Item: map[string]types.AttributeValue{},
						}, nil
					},
					mockPutItem: func(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
						return &dynamodb.PutItemOutput{}, errors.Wrap(&types.ConditionalCheckFailedException{}, "")
					},
				}
			},
			input: SaveInput{
				EmailInput: EmailInput{
					MessageID: "draft-example",
				},
			},
			expectedErr: ErrNotFound,
		},
		{ // with Send
			client: func(t *testing.T) SaveAndSendEmailAPI {
				return mockCreateEmailAPI{
					mockGetItem: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
						return &dynamodb.GetItemOutput{
							Item: map[string]types.AttributeValue{},
						}, nil
					},
					mockPutItem: func(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
						return &dynamodb.PutItemOutput{}, nil
					},
					mockSendEmail: func(ctx context.Context, params *sesv2.SendEmailInput, optFns ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error) {
						return &sesv2.SendEmailOutput{}, errSend
					},
				}
			},
			input: SaveInput{
				EmailInput: EmailInput{
					MessageID: "draft-example",
					From:      []string{""},
				},
				Send: true,
			},
			expectedErr: errSend,
		},
		{ // with Send
			client: func(t *testing.T) SaveAndSendEmailAPI {
				return mockCreateEmailAPI{
					mockGetItem: func(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
						return &dynamodb.GetItemOutput{
							Item: map[string]types.AttributeValue{},
						}, nil
					},
					mockPutItem: func(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
						return &dynamodb.PutItemOutput{}, nil
					},
					mockSendEmail: func(ctx context.Context, params *sesv2.SendEmailInput, optFns ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error) {
						return &sesv2.SendEmailOutput{MessageId: aws.String("sent-message-id")}, nil
					},
					mockTransactWriteItems: func(ctx context.Context, params *dynamodb.TransactWriteItemsInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error) {
						return &dynamodb.TransactWriteItemsOutput{}, errBatchWrite
					},
				}
			},
			input: SaveInput{
				EmailInput: EmailInput{
					MessageID: "draft-example",
					From:      []string{""},
				},
				Send: true,
			},
			expectedErr: errBatchWrite,
		},
	}

	for i, test := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			ctx := context.TODO()

			if test.generateText != nil {
				generateText = test.generateText
			} else {
				generateText = htmlutil.GenerateText
			}

			actual, err := Save(ctx, test.client(t), test.input)

			assert.Equal(t, test.expected, actual)
			assert.Equal(t, test.expectedErr, err)
		})
	}
}
