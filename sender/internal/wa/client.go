package wa

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

const pairDisplayName = "Chrome (Linux)"

type Client struct {
	*whatsmeow.Client
	w *watcher
}

func NewClient(dev *store.Device, log waLog.Logger) *Client {
	cli := whatsmeow.NewClient(dev, log)
	w := newWatcher()
	cli.AddEventHandler(w.handle)
	return &Client{Client: cli, w: w}
}

func (c *Client) Err() error { return c.w.Err() }

func (c *Client) Done() <-chan struct{} { return c.w.Done() }

func (c *Client) ConnectAndWait(ctx context.Context, timeout time.Duration) error {
	if err := c.ConnectContext(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	if err := c.WaitConnected(ctx, timeout); err != nil {
		c.Disconnect()
		return err
	}
	return nil
}

func (c *Client) WaitConnected(ctx context.Context, timeout time.Duration) error {
	return c.w.waitConnected(ctx, timeout)
}

func (c *Client) SelfJIDs() []types.JID {
	var ids []types.JID
	if c.Store.ID != nil {
		ids = append(ids, *c.Store.ID)
	}
	if !c.Store.LID.IsEmpty() {
		ids = append(ids, c.Store.LID)
	}
	return ids
}

func (c *Client) SendText(ctx context.Context, to types.JID, text string) (types.MessageID, error) {
	if err := c.Err(); err != nil {
		return "", err
	}
	resp, err := c.SendMessage(ctx, to, textMessage(text))
	if err != nil {
		return "", fmt.Errorf("send message: %w", errors.Join(err, c.Err()))
	}
	return resp.ID, nil
}

func textMessage(text string) *waE2E.Message {
	return &waE2E.Message{Conversation: proto.String(text)}
}

func (c *Client) PairQR(ctx context.Context, show func(code string)) error {
	ch, err := c.startPairing(ctx)
	if err != nil {
		return err
	}
	return pairLoop(ctx, ch, func(code string) error {
		show(code)
		return nil
	})
}

func (c *Client) PairWithPhone(ctx context.Context, phone string, show func(code string)) error {
	ch, err := c.startPairing(ctx)
	if err != nil {
		return err
	}
	requested := false
	return pairLoop(ctx, ch, func(string) error {
		if requested {
			return nil
		}
		requested = true
		code, err := c.PairPhone(ctx, phone, true, whatsmeow.PairClientChrome, pairDisplayName)
		if err != nil {
			return fmt.Errorf("%w: request pairing code: %w", ErrPairing, err)
		}
		show(code)
		return nil
	})
}

func (c *Client) startPairing(ctx context.Context) (<-chan whatsmeow.QRChannelItem, error) {
	ch, err := c.GetQRChannel(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPairing, err)
	}
	if err := c.ConnectContext(ctx); err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return ch, nil
}
