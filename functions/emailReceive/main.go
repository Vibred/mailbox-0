package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/harryzcy/mailbox/internal/datasource/storage"
	"github.com/harryzcy/mailbox/internal/env"
	"github.com/harryzcy/mailbox/internal/hook"
	"github.com/harryzcy/mailbox/internal/thread"
	"github.com/harryzcy/mailbox/internal/util/format"
)

func main() {
	lambda.Start(handler)
}

func handler(ctx context.Context, sesEvent events.SimpleEmailEvent) error {
	for _, record := range sesEvent.Records {
		ses := record.SES
		fmt.Printf("[%s - %s] Mail = %+v, Receipt = %+v \n", record.EventVersion, record.EventSource, ses.Mail, ses.Receipt)
		receiveEmail(ctx, record.SES)
	}
	return nil
}

const StatusPass = "PASS"

func receiveEmail(ctx context.Context, ses events.SimpleEmailService) {
	fmt.Fprintf(os.Stdout, "received an email from %s\n", ses.Mail.Source)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(env.Region))
	if err != nil {
		fmt.Fprintln(os.Stderr, "unable to load SDK config, ", err)
	}

	item := make(map[string]types.AttributeValue)
	item["DateSent"] = &types.AttributeValueMemberS{Value: format.Date(ses.Mail.CommonHeaders.Date)}

	// YYYY-MM
	typeYearMonth, _ := format.FormatTypeYearMonth("inbox", ses.Mail.Timestamp)
	item["TypeYearMonth"] = &types.AttributeValueMemberS{Value: typeYearMonth}

	item["DateTime"] = &types.AttributeValueMemberS{Value: format.DateTime(ses.Mail.Timestamp)}
	item["MessageID"] = &types.AttributeValueMemberS{Value: ses.Mail.MessageID}                       // Generated by SES
	item["OriginalMessageID"] = &types.AttributeValueMemberS{Value: ses.Mail.CommonHeaders.MessageID} // Original Message-ID from the email
	item["Subject"] = &types.AttributeValueMemberS{Value: ses.Mail.CommonHeaders.Subject}
	item["Source"] = &types.AttributeValueMemberS{Value: ses.Mail.Source}
	item["Destination"] = &types.AttributeValueMemberSS{Value: ses.Mail.Destination}
	item["From"] = &types.AttributeValueMemberSS{Value: ses.Mail.CommonHeaders.From}
	item["To"] = &types.AttributeValueMemberSS{Value: ses.Mail.CommonHeaders.To}
	item["ReturnPath"] = &types.AttributeValueMemberS{Value: ses.Mail.CommonHeaders.ReturnPath}
	item["Verdict"] = &types.AttributeValueMemberM{Value: map[string]types.AttributeValue{
		"Spam":  &types.AttributeValueMemberBOOL{Value: ses.Receipt.SpamVerdict.Status == StatusPass},
		"DKIM":  &types.AttributeValueMemberBOOL{Value: ses.Receipt.DKIMVerdict.Status == StatusPass},
		"DMARC": &types.AttributeValueMemberBOOL{Value: ses.Receipt.DKIMVerdict.Status == StatusPass},
		"SPF":   &types.AttributeValueMemberBOOL{Value: ses.Receipt.SPFVerdict.Status == StatusPass},
		"Virus": &types.AttributeValueMemberBOOL{Value: ses.Receipt.VirusVerdict.Status == StatusPass},
	}}
	item["Unread"] = &types.AttributeValueMemberBOOL{Value: true}

	inReplyTo := ""
	references := ""
	for _, header := range ses.Mail.Headers {
		if header.Name == "Reply-To" {
			item["ReplyTo"] = &types.AttributeValueMemberSS{Value: strings.Split(header.Value, ",")}
		} else if header.Name == "References" {
			item["References"] = &types.AttributeValueMemberS{Value: header.Value}
			references = header.Value
		} else if header.Name == "In-Reply-To" {
			item["InReplyTo"] = &types.AttributeValueMemberS{Value: header.Value}
			inReplyTo = header.Value
		}
	}

	emailResult, err := storage.S3.GetEmail(ctx, s3.NewFromConfig(cfg), ses.Mail.MessageID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get object, %v\n", err)
	}
	item["Text"] = &types.AttributeValueMemberS{Value: emailResult.Text}
	item["HTML"] = &types.AttributeValueMemberS{Value: emailResult.HTML}
	item["Attachments"] = emailResult.Attachments.ToAttributeValue()
	item["Inlines"] = emailResult.Inlines.ToAttributeValue()
	item["OtherParts"] = emailResult.OtherParts.ToAttributeValue()

	fmt.Printf("subject: %v", ses.Mail.CommonHeaders.Subject)

	thread.StoreEmail(ctx, dynamodb.NewFromConfig(cfg), &thread.StoreEmailInput{
		Item:         item,
		InReplyTo:    inReplyTo,
		References:   references,
		TimeReceived: format.RFC3399(ses.Mail.Timestamp),
	})

	err = hook.SendSQS(ctx, sqs.NewFromConfig(cfg), hook.EmailReceipt{
		MessageID: ses.Mail.MessageID,
		Timestamp: ses.Mail.Timestamp.UTC().Format(time.RFC3339),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to send email receipt to SQS, %v\n", err)
	}

	err = hook.SendWebhook(ctx, &hook.Hook{
		Event:  hook.EventEmail,
		Action: hook.ActionReceived,
		Email: hook.Email{
			ID: ses.Mail.MessageID,
		},
		Timestamp: ses.Mail.Timestamp.UTC().Format(time.RFC3339),
	})
	if err != nil {
		log.Printf("failed to send webhook, %v\n", err)
	}
}
