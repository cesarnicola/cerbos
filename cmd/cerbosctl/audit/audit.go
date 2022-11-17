// Copyright 2021-2022 Zenauth Ltd.
// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/alecthomas/chroma"
	"github.com/alecthomas/chroma/formatters"
	"github.com/alecthomas/chroma/lexers"
	"github.com/alecthomas/chroma/styles"
	"github.com/alecthomas/kong"
	"github.com/jwalton/gchalk"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	auditv1 "github.com/cerbos/cerbos/api/genpb/cerbos/audit/v1"
	"github.com/cerbos/cerbos/client"
	cmdclient "github.com/cerbos/cerbos/cmd/cerbosctl/internal/client"
	"github.com/cerbos/cerbos/cmd/cerbosctl/internal/flagset"
)

var newline = []byte("\n")

const (
	dashLen = 54
	help    = `View audit logs.
Requires audit logging to be enabled on the server. Supports several ways of filtering the data.

tail: View the last N records
between: View records captured between two timestamps. The timestamps must be formatted as ISO-8601
since: View records from X hours/minutes/seconds ago to now. Unit suffixes are: h=hours, m=minutes s=seconds
lookup: View a specific record using the Cerbos Call ID

# View the last 10 access logs 
cerbosctl audit --kind=access --tail=10

# View the decision logs from midnight 2021-07-01 to midnight 2021-07-02
cerbosctl audit --kind=decision --between=2021-07-01T00:00:00Z,2021-07-02T00:00:00Z

# View the decision logs from midnight 2021-07-01 to now
cerbosctl audit --kind=decision --between=2021-07-01T00:00:00Z

# View the access logs from 3 hours ago to now as newline-delimited JSON
cerbosctl audit --kind=access --since=3h --raw

# View a specific access log entry by call ID
cerbosctl audit --kind=access --lookup=01F9Y5MFYTX7Y87A30CTJ2FB0S`
)

type Cmd struct {
	Kind string `default:"access" enum:"access,decision" help:"Kind of log entry (${enum})"`
	flagset.AuditFilters
	Raw bool `help:"Output results without formatting or colours"`
}

func (c *Cmd) Run(k *kong.Kong, ctx *cmdclient.Context) error {
	var writer auditLogWriter
	if c.Raw {
		writer = newRawAuditLogWriter(k.Stdout)
	} else {
		writer = newRichAuditLogWriter(k.Stdout)
	}
	defer writer.flush()

	logOptions := c.AuditFilters.GenOptions()

	switch kind := c.Kind; kind {
	case "access":
		logOptions.Type = client.AccessLogs
	case "decision":
		logOptions.Type = client.DecisionLogs
	}

	logs, err := ctx.AdminClient.AuditLogs(context.Background(), logOptions)
	if err != nil {
		return fmt.Errorf("[ERR-151] could not get decision logs: %w", err)
	}

	if err = streamLogsToWriter(writer, logs); err != nil {
		return fmt.Errorf("[ERR-152] could not write decision logs: %w", err)
	}
	return nil
}

func (c *Cmd) Help() string {
	return help
}

func (c *Cmd) Validate() error {
	return c.AuditFilters.Validate()
}

func streamLogsToWriter(writer auditLogWriter, entries <-chan *client.AuditLogEntry) error {
	for e := range entries {
		aLog, err := e.AccessLog()
		if err != nil {
			return fmt.Errorf("[ERR-153] error while receiving access logs: %w", err)
		}
		if aLog != nil {
			if err := writer.write(aLog); err != nil {
				return err
			}
			continue
		}

		dLog, err := e.DecisionLog()
		if err != nil {
			return fmt.Errorf("[ERR-154] error while receiving decision logs: %w", err)
		}
		if dLog != nil {
			if err := writer.write(dLog); err != nil {
				return err
			}
		}
	}

	return nil
}

type auditLogWriter interface {
	write(proto.Message) error
	flush()
}

func newRawAuditLogWriter(out io.Writer) *rawAuditLogWriter {
	return &rawAuditLogWriter{out: out}
}

type rawAuditLogWriter struct {
	out io.Writer
}

func (r *rawAuditLogWriter) write(entry proto.Message) error {
	outBytes, err := protojson.Marshal(entry)
	if err != nil {
		return err
	}

	if _, err := r.out.Write(outBytes); err != nil {
		return err
	}

	_, err = r.out.Write(newline)
	return err
}

func (r *rawAuditLogWriter) flush() {}

type richAuditLogWriter struct {
	out       *bufio.Writer
	lexer     chroma.Lexer
	formatter chroma.Formatter
	rowStyle  func(...string) string
}

func newRichAuditLogWriter(out io.Writer) *richAuditLogWriter {
	lexer := lexers.Get("json")
	if lexer == nil {
		lexer = lexers.Fallback
	}

	var formatter chroma.Formatter
	switch gchalk.GetLevel() {
	case gchalk.LevelAnsi256:
		formatter = formatters.TTY256
	case gchalk.LevelAnsi16m:
		formatter = formatters.TTY16m
	default:
		formatter = formatters.TTY
	}

	return &richAuditLogWriter{
		out:       bufio.NewWriter(out),
		lexer:     chroma.Coalesce(lexer),
		formatter: formatter,
		rowStyle:  gchalk.WithHex("#eeeeee").WithBgHex("#005fff").Bold,
	}
}

func (r *richAuditLogWriter) write(entry proto.Message) error {
	switch e := entry.(type) {
	case *auditv1.AccessLogEntry:
		r.header(fmt.Sprintf("%s %s", e.CallId, strings.Repeat("┈", dashLen)))
		return r.formattedJSON(e)
	case *auditv1.DecisionLogEntry:
		r.header(fmt.Sprintf("%s %s", e.CallId, strings.Repeat("┈", dashLen)))
		return r.formattedJSON(e)
	default:
		return nil
	}
}

func (r *richAuditLogWriter) header(h string) {
	_, _ = r.out.WriteString("\n\n")
	_, _ = r.out.WriteString(r.rowStyle(h))
	_, _ = r.out.WriteString("\n")
}

func (r *richAuditLogWriter) formattedJSON(msg proto.Message) error {
	iterator, err := r.lexer.Tokenise(nil, protojson.Format(msg))
	if err != nil {
		return err
	}

	if err := r.formatter.Format(r.out, styles.SolarizedDark256, iterator); err != nil {
		return err
	}

	_, err = r.out.Write(newline)
	return err
}

func (r *richAuditLogWriter) flush() {
	_ = r.out.Flush()
}
