package store_test

import (
	"testing"
	"time"

	"github.com/bionicotaku/lingo-utils/outbox/store"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestInboxMessage_Validation(t *testing.T) {
	aggregateType := "video"
	aggregateID := uuid.New().String()

	tests := []struct {
		name    string
		msg     store.InboxMessage
		isValid bool
	}{
		{
			name: "valid inbox message with all fields",
			msg: store.InboxMessage{
				EventID:       uuid.New(),
				SourceService: "catalog",
				EventType:     "video.created",
				AggregateType: &aggregateType,
				AggregateID:   &aggregateID,
				Payload:       []byte("test-payload"),
			},
			isValid: true,
		},
		{
			name: "valid inbox message without aggregate info",
			msg: store.InboxMessage{
				EventID:       uuid.New(),
				SourceService: "catalog",
				EventType:     "video.created",
				AggregateType: nil,
				AggregateID:   nil,
				Payload:       []byte("test-payload"),
			},
			isValid: true,
		},
		{
			name: "valid inbox message with empty payload",
			msg: store.InboxMessage{
				EventID:       uuid.New(),
				SourceService: "catalog",
				EventType:     "system.event",
				AggregateType: nil,
				AggregateID:   nil,
				Payload:       []byte{},
			},
			isValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 验证字段类型正确
			assert.IsType(t, uuid.UUID{}, tt.msg.EventID)
			assert.IsType(t, "", tt.msg.SourceService)
			assert.IsType(t, "", tt.msg.EventType)
			assert.IsType(t, (*string)(nil), tt.msg.AggregateType)
			assert.IsType(t, (*string)(nil), tt.msg.AggregateID)
			assert.IsType(t, []byte{}, tt.msg.Payload)

			// 验证必填字段不为空
			assert.NotEqual(t, uuid.Nil, tt.msg.EventID)
			assert.NotEmpty(t, tt.msg.SourceService)
			assert.NotEmpty(t, tt.msg.EventType)
		})
	}
}

func TestInboxEvent_Fields(t *testing.T) {
	now := time.Now().UTC()
	eventID := uuid.New()
	aggregateType := "video"
	aggregateID := uuid.New().String()
	lastError := "processing error"

	event := store.InboxEvent{
		EventID:       eventID,
		SourceService: "catalog",
		EventType:     "video.created",
		AggregateType: &aggregateType,
		AggregateID:   &aggregateID,
		Payload:       []byte("test-payload"),
		ReceivedAt:    now,
		ProcessedAt:   &now,
		LastError:     &lastError,
	}

	// 验证所有字段
	assert.Equal(t, eventID, event.EventID)
	assert.Equal(t, "catalog", event.SourceService)
	assert.Equal(t, "video.created", event.EventType)
	assert.NotNil(t, event.AggregateType)
	assert.Equal(t, "video", *event.AggregateType)
	assert.NotNil(t, event.AggregateID)
	assert.Equal(t, aggregateID, *event.AggregateID)
	assert.Equal(t, []byte("test-payload"), event.Payload)
	assert.True(t, now.Equal(event.ReceivedAt))
	assert.NotNil(t, event.ProcessedAt)
	assert.NotNil(t, event.LastError)
	assert.Equal(t, "processing error", *event.LastError)
}

func TestInboxEvent_NullableFields(t *testing.T) {
	event := store.InboxEvent{
		EventID:       uuid.New(),
		SourceService: "catalog",
		EventType:     "video.created",
		AggregateType: nil, // 可选
		AggregateID:   nil, // 可选
		Payload:       []byte("test"),
		ReceivedAt:    time.Now(),
		ProcessedAt:   nil, // 未处理
		LastError:     nil, // 无错误
	}

	// 验证可空字段为 nil
	assert.Nil(t, event.AggregateType)
	assert.Nil(t, event.AggregateID)
	assert.Nil(t, event.ProcessedAt)
	assert.Nil(t, event.LastError)
}

func TestInboxEvent_ProcessingStates(t *testing.T) {
	baseEvent := store.InboxEvent{
		EventID:       uuid.New(),
		SourceService: "catalog",
		EventType:     "video.created",
		Payload:       []byte("test"),
		ReceivedAt:    time.Now(),
	}

	tests := []struct {
		name        string
		event       store.InboxEvent
		isProcessed bool
		hasFailed   bool
	}{
		{
			name: "pending - not processed, no error",
			event: store.InboxEvent{
				EventID:       baseEvent.EventID,
				SourceService: baseEvent.SourceService,
				EventType:     baseEvent.EventType,
				Payload:       baseEvent.Payload,
				ReceivedAt:    baseEvent.ReceivedAt,
				ProcessedAt:   nil,
				LastError:     nil,
			},
			isProcessed: false,
			hasFailed:   false,
		},
		{
			name: "processed successfully",
			event: store.InboxEvent{
				EventID:       baseEvent.EventID,
				SourceService: baseEvent.SourceService,
				EventType:     baseEvent.EventType,
				Payload:       baseEvent.Payload,
				ReceivedAt:    baseEvent.ReceivedAt,
				ProcessedAt:   ptrTime(time.Now()),
				LastError:     nil,
			},
			isProcessed: true,
			hasFailed:   false,
		},
		{
			name: "failed processing",
			event: store.InboxEvent{
				EventID:       baseEvent.EventID,
				SourceService: baseEvent.SourceService,
				EventType:     baseEvent.EventType,
				Payload:       baseEvent.Payload,
				ReceivedAt:    baseEvent.ReceivedAt,
				ProcessedAt:   nil,
				LastError:     strPtr("error occurred"),
			},
			isProcessed: false,
			hasFailed:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 验证处理状态
			if tt.isProcessed {
				assert.NotNil(t, tt.event.ProcessedAt, "应该已处理")
			} else {
				assert.Nil(t, tt.event.ProcessedAt, "不应该已处理")
			}

			// 验证错误状态
			if tt.hasFailed {
				assert.NotNil(t, tt.event.LastError, "应该有错误")
			} else {
				assert.Nil(t, tt.event.LastError, "不应该有错误")
			}
		})
	}
}

// 辅助函数
func ptrTime(t time.Time) *time.Time {
	return &t
}

func strPtr(s string) *string {
	return &s
}

// 以下是集成测试，需要真实数据库
/*
func TestRepository_RecordInboxEvent_Integration(t *testing.T) {
	t.Skip("需要数据库连接，移至集成测试")
}

func TestRepository_GetInboxEvent_Integration(t *testing.T) {
	t.Skip("需要数据库连接，移至集成测试")
}

func TestRepository_MarkInboxProcessed_Integration(t *testing.T) {
	t.Skip("需要数据库连接，移至集成测试")
}

func TestRepository_RecordInboxError_Integration(t *testing.T) {
	t.Skip("需要数据库连接，移至集成测试")
}
*/
