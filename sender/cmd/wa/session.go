package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"

	"cp-reminders/sender/internal/drain"
	"cp-reminders/sender/internal/state"
	"cp-reminders/sender/internal/wa"
)

type conn struct {
	sess *state.Session
	cli  *wa.Client
}

var (
	_ drain.Session = (*conn)(nil)
	_ releaser      = (*conn)(nil)
)

func dial(ctx context.Context, dir string, v *state.Vault, log waLog.Logger, connectTimeout time.Duration) (*conn, error) {
	sess, err := state.OpenSession(ctx, dir, v, log.Sub("Store"))
	if err != nil {
		return nil, err
	}
	dev, err := sess.PairedDevice(ctx)
	if err != nil {
		return nil, errors.Join(err, sess.Discard())
	}
	cli := wa.NewClient(dev, log.Sub("Client"))
	if err := cli.ConnectAndWait(ctx, connectTimeout); err != nil {
		return nil, errors.Join(err, sess.Commit())
	}
	return &conn{sess: sess, cli: cli}, nil
}

func (c *conn) Close() error {
	c.cli.Disconnect()
	return c.sess.Save()
}

func (c *conn) Release() error {
	return c.sess.Release()
}

func (c *conn) Group(ctx context.Context, jid string) (drain.Group, error) {
	target, err := types.ParseJID(jid)
	if err != nil {
		return drain.Group{}, fmt.Errorf("parse %s: %w", envAnnounceJID, err)
	}
	info, err := c.cli.GetGroupInfo(ctx, target)
	if err != nil {
		return drain.Group{}, fmt.Errorf("get group info: %w", err)
	}
	return drain.Group{
		IsAnnouncements: info.IsDefaultSubGroup,
		IsAdmin:         wa.IsAdmin(info, c.cli.SelfJIDs()...),
	}, nil
}

func (c *conn) SendText(ctx context.Context, jid, text string) (string, error) {
	target, err := types.ParseJID(jid)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", envAnnounceJID, err)
	}
	id, err := c.cli.SendText(ctx, target, text)
	return string(id), err
}
