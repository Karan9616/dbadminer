package dbadminer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/dbadminer/dbadminer/internal/guard"
)

type mutateResult struct {
	RowsAffected int64  `json:"rowsAffected"`
	SQL          string `json:"sql,omitempty"`
}

type rowValuesReq struct {
	Values map[string]any `json:"values"`
	Key    map[string]any `json:"key"`
}

func (e *Explorer) requireWrite(w http.ResponseWriter) bool {
	if e.cfg.ReadOnly {
		e.fail(w, http.StatusForbidden, "writes require full access")
		return false
	}
	return true
}

func (e *Explorer) decodeJSONBody(w http.ResponseWriter, r *http.Request, dest any) bool {
	if !jsonContentType(r) {
		e.fail(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	body := http.MaxBytesReader(w, r.Body, e.cfg.MaxBodyBytes)
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(body)
	if err := dec.Decode(dest); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) || strings.Contains(err.Error(), "request body too large") {
			e.fail(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		e.fail(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func (e *Explorer) execSQL(ctx context.Context, q queryer, sqlText string, args ...any) (int64, error) {
	res, err := q.ExecContext(ctx, sqlText, args...)
	e.audit(ctx, sqlText, err)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return n, nil
}

func (e *Explorer) handleSchema(w http.ResponseWriter, r *http.Request) {
	schema := r.PathValue("schema")
	table := r.PathValue("table")
	var payload schemaPayload
	err := e.withQueryer(r.Context(), func(ctx context.Context, q queryer) error {
		if err := e.ensureTable(ctx, q, schema, table); err != nil {
			return err
		}
		var err error
		payload, err = e.loadSchema(ctx, q, schema, table)
		return err
	})
	if err != nil {
		e.failErr(w, err)
		return
	}
	e.writeJSON(w, http.StatusOK, payload)
}

func (e *Explorer) handleCreateTable(w http.ResponseWriter, r *http.Request) {
	if !e.requireWrite(w) {
		return
	}
	var req createTableReq
	if !e.decodeJSONBody(w, r, &req) {
		return
	}
	if req.Schema == "" {
		req.Schema = e.defaultSchema()
	}
	sqlText, err := createTableSQL(e.dialect, req.Schema, req.Name, req.Columns)
	if err != nil {
		e.failErr(w, err)
		return
	}
	var n int64
	err = e.withQueryer(r.Context(), func(ctx context.Context, q queryer) error {
		var err error
		n, err = e.execSQL(ctx, q, sqlText)
		return err
	})
	if err != nil {
		e.failErr(w, err)
		return
	}
	e.writeJSON(w, http.StatusOK, mutateResult{RowsAffected: n, SQL: sqlText})
}

func (e *Explorer) handleCreateView(w http.ResponseWriter, r *http.Request) {
	if !e.requireWrite(w) {
		return
	}
	var req createViewReq
	if !e.decodeJSONBody(w, r, &req) {
		return
	}
	if req.Schema == "" {
		req.Schema = e.defaultSchema()
	}
	ok, err := guard.ReadOnlySQL(req.Select)
	if !ok {
		msg := "view select must be a single read-only statement"
		if err != nil {
			msg = err.Error()
		}
		e.fail(w, http.StatusBadRequest, msg)
		return
	}
	sqlText, err := createViewSQL(e.dialect, req.Schema, req.Name, req.Select)
	if err != nil {
		e.failErr(w, err)
		return
	}
	var n int64
	err = e.withQueryer(r.Context(), func(ctx context.Context, q queryer) error {
		var err error
		n, err = e.execSQL(ctx, q, sqlText)
		return err
	})
	if err != nil {
		e.failErr(w, err)
		return
	}
	e.writeJSON(w, http.StatusOK, mutateResult{RowsAffected: n, SQL: sqlText})
}

func (e *Explorer) handleAlterTable(w http.ResponseWriter, r *http.Request) {
	if !e.requireWrite(w) {
		return
	}
	schema := r.PathValue("schema")
	table := r.PathValue("table")
	var req alterTableReq
	if !e.decodeJSONBody(w, r, &req) {
		return
	}
	stmts, err := alterOpsSQL(e.dialect, schema, table, req.Ops)
	if err != nil {
		e.failErr(w, err)
		return
	}
	var total int64
	err = e.withQueryer(r.Context(), func(ctx context.Context, q queryer) error {
		if err := e.ensureTable(ctx, q, schema, table); err != nil {
			return err
		}
		for _, sqlText := range stmts {
			n, err := e.execSQL(ctx, q, sqlText)
			if err != nil {
				return err
			}
			total += n
		}
		return nil
	})
	if err != nil {
		e.failErr(w, err)
		return
	}
	e.writeJSON(w, http.StatusOK, mutateResult{RowsAffected: total, SQL: strings.Join(stmts, ";\n")})
}

func (e *Explorer) handleDropTable(w http.ResponseWriter, r *http.Request) {
	if !e.requireWrite(w) {
		return
	}
	schema := r.PathValue("schema")
	table := r.PathValue("table")
	kind := r.URL.Query().Get("type")
	if kind == "" {
		kind = "table"
	}
	sqlText, err := dropRelationSQL(e.dialect, schema, table, kind)
	if err != nil {
		e.failErr(w, err)
		return
	}
	var n int64
	err = e.withQueryer(r.Context(), func(ctx context.Context, q queryer) error {
		if err := e.ensureTable(ctx, q, schema, table); err != nil {
			return err
		}
		var err error
		n, err = e.execSQL(ctx, q, sqlText)
		return err
	})
	if err != nil {
		e.failErr(w, err)
		return
	}
	e.writeJSON(w, http.StatusOK, mutateResult{RowsAffected: n, SQL: sqlText})
}

func (e *Explorer) handleCreateIndex(w http.ResponseWriter, r *http.Request) {
	if !e.requireWrite(w) {
		return
	}
	schema := r.PathValue("schema")
	table := r.PathValue("table")
	var req createIndexReq
	if !e.decodeJSONBody(w, r, &req) {
		return
	}
	sqlText, err := createIndexSQL(e.dialect, schema, table, req)
	if err != nil {
		e.failErr(w, err)
		return
	}
	var n int64
	err = e.withQueryer(r.Context(), func(ctx context.Context, q queryer) error {
		if err := e.ensureTable(ctx, q, schema, table); err != nil {
			return err
		}
		var err error
		n, err = e.execSQL(ctx, q, sqlText)
		return err
	})
	if err != nil {
		e.failErr(w, err)
		return
	}
	e.writeJSON(w, http.StatusOK, mutateResult{RowsAffected: n, SQL: sqlText})
}

func (e *Explorer) handleDropIndex(w http.ResponseWriter, r *http.Request) {
	if !e.requireWrite(w) {
		return
	}
	schema := r.PathValue("schema")
	table := r.PathValue("table")
	name := r.PathValue("name")
	sqlText, err := dropIndexSQL(e.dialect, schema, table, name)
	if err != nil {
		e.failErr(w, err)
		return
	}
	var n int64
	err = e.withQueryer(r.Context(), func(ctx context.Context, q queryer) error {
		if err := e.ensureTable(ctx, q, schema, table); err != nil {
			return err
		}
		var err error
		n, err = e.execSQL(ctx, q, sqlText)
		return err
	})
	if err != nil {
		e.failErr(w, err)
		return
	}
	e.writeJSON(w, http.StatusOK, mutateResult{RowsAffected: n, SQL: sqlText})
}

func (e *Explorer) handleCreateForeignKey(w http.ResponseWriter, r *http.Request) {
	if !e.requireWrite(w) {
		return
	}
	schema := r.PathValue("schema")
	table := r.PathValue("table")
	var req createFKReq
	if !e.decodeJSONBody(w, r, &req) {
		return
	}
	if req.RefSchema == "" {
		req.RefSchema = schema
	}
	sqlText, err := createForeignKeySQL(e.dialect, schema, table, req)
	if err != nil {
		e.failErr(w, err)
		return
	}
	var n int64
	err = e.withQueryer(r.Context(), func(ctx context.Context, q queryer) error {
		if err := e.ensureTable(ctx, q, schema, table); err != nil {
			return err
		}
		var err error
		n, err = e.execSQL(ctx, q, sqlText)
		return err
	})
	if err != nil {
		e.failErr(w, err)
		return
	}
	e.writeJSON(w, http.StatusOK, mutateResult{RowsAffected: n, SQL: sqlText})
}

func (e *Explorer) handleDropForeignKey(w http.ResponseWriter, r *http.Request) {
	if !e.requireWrite(w) {
		return
	}
	schema := r.PathValue("schema")
	table := r.PathValue("table")
	name := r.PathValue("name")
	sqlText, err := dropForeignKeySQL(e.dialect, schema, table, name)
	if err != nil {
		e.failErr(w, err)
		return
	}
	var n int64
	err = e.withQueryer(r.Context(), func(ctx context.Context, q queryer) error {
		if err := e.ensureTable(ctx, q, schema, table); err != nil {
			return err
		}
		var err error
		n, err = e.execSQL(ctx, q, sqlText)
		return err
	})
	if err != nil {
		e.failErr(w, err)
		return
	}
	e.writeJSON(w, http.StatusOK, mutateResult{RowsAffected: n, SQL: sqlText})
}

func (e *Explorer) handleInsertRow(w http.ResponseWriter, r *http.Request) {
	if !e.requireWrite(w) {
		return
	}
	schema := r.PathValue("schema")
	table := r.PathValue("table")
	var req rowValuesReq
	if !e.decodeJSONBody(w, r, &req) {
		return
	}
	var n int64
	var sqlText string
	err := e.withQueryer(r.Context(), func(ctx context.Context, q queryer) error {
		if err := e.ensureTable(ctx, q, schema, table); err != nil {
			return err
		}
		cols, pk, err := e.loadColumns(ctx, q, schema, table)
		if err != nil {
			return err
		}
		if len(pk) == 0 {
			return &httpError{status: http.StatusBadRequest, msg: "table has no primary key; use the SQL console"}
		}
		names, args, err := mapValues(cols, req.Values, false)
		if err != nil {
			return err
		}
		sqlText, _, err = insertRowSQL(e.dialect, schema, table, names)
		if err != nil {
			return err
		}
		n, err = e.execSQL(ctx, q, sqlText, args...)
		return err
	})
	if err != nil {
		e.failErr(w, err)
		return
	}
	e.writeJSON(w, http.StatusOK, mutateResult{RowsAffected: n, SQL: sqlText})
}

func (e *Explorer) handleUpdateRow(w http.ResponseWriter, r *http.Request) {
	if !e.requireWrite(w) {
		return
	}
	schema := r.PathValue("schema")
	table := r.PathValue("table")
	var req rowValuesReq
	if !e.decodeJSONBody(w, r, &req) {
		return
	}
	var n int64
	var sqlText string
	err := e.withQueryer(r.Context(), func(ctx context.Context, q queryer) error {
		if err := e.ensureTable(ctx, q, schema, table); err != nil {
			return err
		}
		cols, pk, err := e.loadColumns(ctx, q, schema, table)
		if err != nil {
			return err
		}
		if len(pk) == 0 {
			return &httpError{status: http.StatusBadRequest, msg: "table has no primary key; use the SQL console"}
		}
		setNames, setArgs, err := mapValues(cols, req.Values, false)
		if err != nil {
			return err
		}
		keyNames, keyArgs, err := mapKey(pk, req.Key)
		if err != nil {
			return err
		}
		sqlText, err = updateRowSQL(e.dialect, schema, table, setNames, keyNames)
		if err != nil {
			return err
		}
		args := append(setArgs, keyArgs...)
		n, err = e.execSQL(ctx, q, sqlText, args...)
		return err
	})
	if err != nil {
		e.failErr(w, err)
		return
	}
	e.writeJSON(w, http.StatusOK, mutateResult{RowsAffected: n, SQL: sqlText})
}

func (e *Explorer) handleDeleteRow(w http.ResponseWriter, r *http.Request) {
	if !e.requireWrite(w) {
		return
	}
	schema := r.PathValue("schema")
	table := r.PathValue("table")
	var req rowValuesReq
	if !e.decodeJSONBody(w, r, &req) {
		return
	}
	var n int64
	var sqlText string
	err := e.withQueryer(r.Context(), func(ctx context.Context, q queryer) error {
		if err := e.ensureTable(ctx, q, schema, table); err != nil {
			return err
		}
		_, pk, err := e.loadColumns(ctx, q, schema, table)
		if err != nil {
			return err
		}
		if len(pk) == 0 {
			return &httpError{status: http.StatusBadRequest, msg: "table has no primary key; use the SQL console"}
		}
		keyNames, keyArgs, err := mapKey(pk, req.Key)
		if err != nil {
			return err
		}
		sqlText, err = deleteRowSQL(e.dialect, schema, table, keyNames)
		if err != nil {
			return err
		}
		n, err = e.execSQL(ctx, q, sqlText, keyArgs...)
		return err
	})
	if err != nil {
		e.failErr(w, err)
		return
	}
	e.writeJSON(w, http.StatusOK, mutateResult{RowsAffected: n, SQL: sqlText})
}

func (e *Explorer) defaultSchema() string {
	if e.cfg.Schema != "" {
		return e.cfg.Schema
	}
	switch e.dialect.Name() {
	case "postgres":
		return "public"
	case "sqlite":
		return "main"
	default:
		return ""
	}
}

func mapValues(cols []columnInfo, values map[string]any, requireAll bool) ([]string, []any, error) {
	if values == nil {
		return nil, nil, &httpError{status: http.StatusBadRequest, msg: "values are required"}
	}
	known := map[string]struct{}{}
	for _, c := range cols {
		known[c.Name] = struct{}{}
	}
	names := make([]string, 0, len(values))
	args := make([]any, 0, len(values))
	for name, val := range values {
		if _, ok := known[name]; !ok {
			return nil, nil, &httpError{status: http.StatusBadRequest, msg: "unknown column: " + name}
		}
		names = append(names, name)
		args = append(args, val)
	}
	if len(names) == 0 {
		return nil, nil, &httpError{status: http.StatusBadRequest, msg: "values are required"}
	}
	if requireAll && len(names) != len(cols) {
		return nil, nil, &httpError{status: http.StatusBadRequest, msg: "all columns are required"}
	}
	return names, args, nil
}

func mapKey(pk []string, key map[string]any) ([]string, []any, error) {
	if key == nil {
		return nil, nil, &httpError{status: http.StatusBadRequest, msg: "key is required"}
	}
	names := make([]string, 0, len(pk))
	args := make([]any, 0, len(pk))
	for _, name := range pk {
		val, ok := key[name]
		if !ok {
			return nil, nil, &httpError{status: http.StatusBadRequest, msg: "missing primary key column: " + name}
		}
		names = append(names, name)
		args = append(args, val)
	}
	if len(key) != len(pk) {
		return nil, nil, &httpError{status: http.StatusBadRequest, msg: "key must contain only primary key columns"}
	}
	return names, args, nil
}
