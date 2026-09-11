package snmp

import (
	"context"
	"errors"
	"fmt"

	"github.com/gosnmp/gosnmp"
)

// Record walks everything a device answers under each of roots and hands every
// varbind to keep, in the order the device gives them: the recording a
// simulator model is made from (app_simrecord.go). Unlike Walk it keeps the
// varbinds as gosnmp decoded them — their types are what a recording is for —,
// walks one device, and stops when ctx is done or keep returns an error.
func (c *Client) Record(ctx context.Context, req SnmpRequest, roots []string, keep func(gosnmp.SnmpPDU) error) error {
	if len(req.Targets) != 1 {
		return errors.New("a recording walks one device")
	}
	g, err := c.newGoSNMP(req.Targets[0], req.Community, req.Version, req.Port, req.Timeout, req.Retries, req.V3)
	if err != nil {
		return err
	}
	g.Context = ctx
	if err := g.Connect(); err != nil {
		return fmt.Errorf("connect failed: %v", err)
	}
	defer g.Conn.Close()
	walk := g.BulkWalk
	if g.Version == gosnmp.Version1 {
		walk = g.Walk
	}
	for _, root := range roots {
		err := walk(root, func(pdu gosnmp.SnmpPDU) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			return keep(pdu)
		})
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			return fmt.Errorf("walking %s: %w", root, err)
		}
	}
	return nil
}
