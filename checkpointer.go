package ethindex

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
)

const (
	checkpointFile    = "checkpoint.gz"
	checkpointTmpFile = "checkpoint.tmp.gz"
)

// Checkpointer stages and commits checkpoints.
type Checkpointer struct {
	s BlobStore

	staged     BlockRef
	stagedHash [32]byte
	pending    chan error
}

// NewCheckpointer returns a Checkpointer backed by s.
func NewCheckpointer(s BlobStore) *Checkpointer {
	return &Checkpointer{s: s}
}

// waitPending waits for the pending staged checkpoint save.
func (c *Checkpointer) waitPending(ctx context.Context) error {
	if c.pending == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-c.pending:
		c.pending = nil
		return err
	}
}

// Staged returns the currently staged checkpoint head.
func (c *Checkpointer) Staged() *BlockRef {
	return &c.staged
}

// Load returns the committed checkpoint.
func (c *Checkpointer) Load(ctx context.Context) (*Checkpoint, error) {
	bin, err := c.s.Read(checkpointFile)
	if err != nil {
		return nil, fmt.Errorf("store read: %w", err)
	}
	if len(bin) == 0 {
		return nil, nil
	}

	var cp Checkpoint
	if err := cp.UnmarshalBinary(bin); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	return &cp, err
}

// Stage saves cp as the next checkpoint to commit.
func (c *Checkpointer) Stage(ctx context.Context, cp Checkpoint) error {
	if err := c.waitPending(ctx); err != nil {
		return err
	}

	c.pending = make(chan error, 1)

	go func() (err error) {
		defer func() { c.pending <- err }()

		bin, err := cp.MarshalBinary()
		if err != nil {
			return fmt.Errorf("marshal: %w", err)
		}
		hash := sha256.Sum256(bin)

		if err := c.s.Write(checkpointTmpFile, bin); err != nil {
			return fmt.Errorf("store write: %w", err)
		}

		c.staged = cp.Head
		c.stagedHash = hash

		return nil
	}()

	return nil
}

// Commit promotes the staged checkpoint to committed.
func (c *Checkpointer) Commit(ctx context.Context) error {
	if err := c.waitPending(ctx); err != nil {
		return err
	}

	bin, err := c.s.Read(checkpointTmpFile)
	if err != nil {
		return fmt.Errorf("store read: %w", err)
	}

	if c.stagedHash != sha256.Sum256(bin) {
		return errors.New("staged checkpoint changed")
	}

	if err := c.s.Move(checkpointTmpFile, checkpointFile); err != nil {
		return fmt.Errorf("store move: %w", err)
	}

	c.staged = BlockRef{}
	c.stagedHash = [32]byte{}

	return nil
}

// Close waits for any pending checkpoint save.
func (c *Checkpointer) Close(ctx context.Context) error {
	return c.waitPending(ctx)
}
