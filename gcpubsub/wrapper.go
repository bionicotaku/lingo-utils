package gcpubsub

import "context"

// Publish 是 Component 提供的便捷发布方法。
func (c *Component) Publish(ctx context.Context, msg Message) (string, error) {
	return c.publisher.Publish(ctx, msg)
}

// Receive 是 Component 提供的便捷消费方法。
func (c *Component) Receive(ctx context.Context, handler func(context.Context, *Message) error) error {
	return c.subscriber.Receive(ctx, handler)
}

// FlushPublisher 触发发布端刷新并停止 Topic。
func (c *Component) FlushPublisher(ctx context.Context) error {
	return c.publisher.Flush(ctx)
}
