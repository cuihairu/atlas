// Audit logging for the gRPC transport, completing the parity started by
// the auth interceptor: over REST every Admin operation lands in the audit
// ring (GET /v1/admin/audit); without this layer the same Disable /
// RollbackMigration operations arriving over gRPC left no trace. Entries go
// into the same httpapi.AuditLog ring — one ring, one viewer endpoint, one
// structured-log vocabulary — with the REST field semantics mapped onto
// RPCs: read RPCs record like GETs (no diff), mutating RPCs like POSTs with
// the request proto (protojson) as the change record, and the gRPC status
// code is mapped to the HTTP status the REST API would have returned.
//
// Attach this interceptor AFTER UnaryAuth (audit inside auth): rejected
// calls never reach it, exactly like REST 401/403s never reach the audit
// middleware. Without auth configured it still records in dev mode, with
// the same ":anonymous" actor the REST ring shows in that mode.
package grpc

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	atlashttpapi "github.com/cuihairu/atlas/internal/httpapi"
)

// httpStatusOf maps a gRPC status code onto the HTTP status the REST admin
// API would have produced for the same failure (the google.rpc convention),
// so consumers of the audit ring read one status vocabulary.
func httpStatusOf(code codes.Code) int {
	switch code {
	case codes.OK:
		return 200
	case codes.Canceled:
		return 499
	case codes.InvalidArgument:
		return 400
	case codes.DeadlineExceeded:
		return 504
	case codes.NotFound:
		return 404
	case codes.AlreadyExists, codes.Aborted:
		return 409
	case codes.PermissionDenied:
		return 403
	case codes.ResourceExhausted:
		return 429
	case codes.FailedPrecondition, codes.OutOfRange:
		return 400
	case codes.Unimplemented:
		return 501
	case codes.Unavailable:
		return 503
	case codes.Unauthenticated:
		return 401
	default: // Unknown, Internal, DataLoss
		return 500
	}
}

// UnaryAudit returns the interceptor recording Admin-domain RPCs into audit.
// Reads (the adminReadMethods set) are recorded without a diff; mutating
// RPCs carry the protojson-marshalled request, truncated to the log's body
// bound — valid JSON kept as-is, anything else wrapped as a string, same as
// the REST middleware. The actor comes from the context the auth
// interceptor populated ("role:keyfingerprint", ":anonymous" in dev mode).
func UnaryAudit(audit *atlashttpapi.AuditLog) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if authDomain(info.FullMethod) != "admin" {
			return handler(ctx, req)
		}

		resp, err := handler(ctx, req)

		method := "GET"
		var body json.RawMessage
		if !adminReadMethods[strings.TrimPrefix(info.FullMethod, adminPrefix)] {
			method = "POST"
			if msg, ok := req.(proto.Message); ok {
				if raw, merr := protojson.Marshal(msg); merr == nil {
					body = truncateAuditBody(raw, audit.MaxBody())
				}
			}
		}

		code := status.Code(err)
		audit.Record(atlashttpapi.AuditEntry{
			Time:   time.Now().UTC(),
			Actor:  atlashttpapi.ActorFrom(ctx).String(),
			Method: method,
			Path:   info.FullMethod,
			Status: httpStatusOf(code),
			Body:   body,
		})
		return resp, err
	}
}

// truncateAuditBody applies the log's payload bound and the REST
// keep-valid-JSON-else-wrap-as-string rule to a marshalled request.
func truncateAuditBody(raw []byte, maxBody int) json.RawMessage {
	if maxBody > 0 && len(raw) > maxBody {
		raw = raw[:maxBody]
	}
	if json.Valid(raw) {
		return json.RawMessage(raw)
	}
	if wrapped, err := json.Marshal(string(raw)); err == nil {
		return wrapped
	}
	return nil
}
