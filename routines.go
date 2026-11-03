package dbadminer

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
)

type routineInfo struct {
	Schema       string `json:"schema"`
	Name         string `json:"name"`
	Type         string `json:"type"` // procedure | function
	Language     string `json:"language,omitempty"`
	ReturnType   string `json:"returnType,omitempty"`
	IdentityArgs string `json:"identityArgs,omitempty"`
}

type routineParam struct {
	Name     string `json:"name"`
	Mode     string `json:"mode,omitempty"`
	DataType string `json:"dataType"`
	Position int    `json:"position"`
}

type routineDetail struct {
	routineInfo
	Parameters []routineParam `json:"parameters"`
	Definition string         `json:"definition"`
}

type routinesPayload struct {
	Routines []routineInfo `json:"routines"`
}

func normalizeRoutineType(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "PROCEDURE":
		return "procedure"
	case "FUNCTION", "AGGREGATE":
		return "function"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func routinesListQuery(d Dialect) (string, bool) {
	switch d.Name() {
	case "postgres":
		return `
SELECT n.nspname,
       p.proname,
       CASE p.prokind WHEN 'p' THEN 'PROCEDURE' ELSE 'FUNCTION' END,
       l.lanname,
       COALESCE(pg_catalog.pg_get_function_result(p.oid), ''),
       COALESCE(pg_catalog.pg_get_function_identity_arguments(p.oid), '')
FROM pg_catalog.pg_proc AS p
JOIN pg_catalog.pg_namespace AS n ON n.oid = p.pronamespace
JOIN pg_catalog.pg_language AS l ON l.oid = p.prolang
WHERE p.prokind IN ('f', 'p')
  AND n.nspname <> 'pg_catalog'
  AND n.nspname <> 'information_schema'
  AND n.nspname NOT LIKE 'pg_toast%'
  AND n.nspname NOT LIKE 'pg_temp%'
ORDER BY n.nspname, p.proname, 6`, true
	case "mysql", "mariadb":
		return `
SELECT routine_schema,
       routine_name,
       routine_type,
       COALESCE(external_language, 'SQL'),
       COALESCE(dtd_identifier, ''),
       ''
FROM information_schema.routines
WHERE routine_schema NOT IN ('information_schema', 'mysql', 'performance_schema', 'sys')
  AND routine_type IN ('PROCEDURE', 'FUNCTION')
ORDER BY routine_schema, routine_name, routine_type`, true
	default:
		return "", false
	}
}

func (e *Explorer) handleRoutines(w http.ResponseWriter, r *http.Request) {
	var routines []routineInfo
	err := e.withQueryer(r.Context(), func(ctx context.Context, q queryer) error {
		var err error
		routines, err = e.listRoutines(ctx, q)
		return err
	})
	if err != nil {
		e.failErr(w, err)
		return
	}
	if routines == nil {
		routines = []routineInfo{}
	}
	e.writeJSON(w, http.StatusOK, routinesPayload{Routines: routines})
}

func (e *Explorer) handleRoutine(w http.ResponseWriter, r *http.Request) {
	schema := r.PathValue("schema")
	name := r.PathValue("name")
	kind := normalizeRoutineType(r.URL.Query().Get("type"))
	args := r.URL.Query().Get("args")
	if kind != "procedure" && kind != "function" {
		e.fail(w, http.StatusBadRequest, "type must be procedure or function")
		return
	}
	var detail routineDetail
	err := e.withQueryer(r.Context(), func(ctx context.Context, q queryer) error {
		var err error
		detail, err = e.loadRoutine(ctx, q, schema, name, kind, args)
		return err
	})
	if err != nil {
		e.failErr(w, err)
		return
	}
	e.writeJSON(w, http.StatusOK, detail)
}

func (e *Explorer) listRoutines(ctx context.Context, q queryer) ([]routineInfo, error) {
	query, ok := routinesListQuery(e.dialect)
	if !ok {
		return []routineInfo{}, nil
	}
	rows, err := e.query(ctx, q, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []routineInfo
	for rows.Next() {
		var schema, name, kind, language, returnType, identityArgs string
		if err := rows.Scan(&schema, &name, &kind, &language, &returnType, &identityArgs); err != nil {
			return nil, err
		}
		if e.cfg.Schema != "" && schema != e.cfg.Schema {
			continue
		}
		info := routineInfo{
			Schema:       schema,
			Name:         name,
			Type:         normalizeRoutineType(kind),
			Language:     language,
			ReturnType:   strings.TrimSpace(returnType),
			IdentityArgs: identityArgs,
		}
		if info.Type == "procedure" {
			info.ReturnType = ""
		}
		out = append(out, info)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []routineInfo{}
	}
	return out, nil
}

func (e *Explorer) loadRoutine(ctx context.Context, q queryer, schema, name, kind, args string) (routineDetail, error) {
	if schema == "" || name == "" || strings.Contains(schema, "\x00") || strings.Contains(name, "\x00") {
		return routineDetail{}, errNotFound
	}
	if e.cfg.Schema != "" && schema != e.cfg.Schema {
		return routineDetail{}, errNotFound
	}
	switch e.dialect.Name() {
	case "postgres":
		return e.loadPostgresRoutine(ctx, q, schema, name, kind, args)
	case "mysql", "mariadb":
		return e.loadMySQLRoutine(ctx, q, schema, name, kind)
	default:
		return routineDetail{}, errNotFound
	}
}

func (e *Explorer) loadPostgresRoutine(ctx context.Context, q queryer, schema, name, kind, args string) (routineDetail, error) {
	prokind := "f"
	if kind == "procedure" {
		prokind = "p"
	}
	const findOID = `
SELECT p.oid,
       l.lanname,
       COALESCE(pg_catalog.pg_get_function_result(p.oid), ''),
       COALESCE(pg_catalog.pg_get_function_identity_arguments(p.oid), '')
FROM pg_catalog.pg_proc AS p
JOIN pg_catalog.pg_namespace AS n ON n.oid = p.pronamespace
JOIN pg_catalog.pg_language AS l ON l.oid = p.prolang
WHERE n.nspname = $1
  AND p.proname = $2
  AND p.prokind = $3
  AND COALESCE(pg_catalog.pg_get_function_identity_arguments(p.oid), '') = $4`
	var oid int64
	var language, returnType, identityArgs string
	err := q.QueryRowContext(ctx, findOID, schema, name, prokind, args).Scan(&oid, &language, &returnType, &identityArgs)
	if err == sql.ErrNoRows {
		// Fall back when only one overload exists and args omitted.
		if args == "" {
			const findOne = `
SELECT p.oid,
       l.lanname,
       COALESCE(pg_catalog.pg_get_function_result(p.oid), ''),
       COALESCE(pg_catalog.pg_get_function_identity_arguments(p.oid), '')
FROM pg_catalog.pg_proc AS p
JOIN pg_catalog.pg_namespace AS n ON n.oid = p.pronamespace
JOIN pg_catalog.pg_language AS l ON l.oid = p.prolang
WHERE n.nspname = $1
  AND p.proname = $2
  AND p.prokind = $3`
			rows, qerr := e.query(ctx, q, findOne, schema, name, prokind)
			if qerr != nil {
				return routineDetail{}, qerr
			}
			defer func() { _ = rows.Close() }()
			if !rows.Next() {
				return routineDetail{}, errNotFound
			}
			if err := rows.Scan(&oid, &language, &returnType, &identityArgs); err != nil {
				return routineDetail{}, err
			}
			if rows.Next() {
				return routineDetail{}, &httpError{status: 400, msg: "ambiguous routine; pass args= for the overload"}
			}
			if err := rows.Err(); err != nil {
				return routineDetail{}, err
			}
		} else {
			return routineDetail{}, errNotFound
		}
	} else if err != nil {
		return routineDetail{}, err
	}

	var definition string
	if err := q.QueryRowContext(ctx, `SELECT pg_catalog.pg_get_functiondef($1::oid)`, oid).Scan(&definition); err != nil {
		return routineDetail{}, err
	}
	params, err := e.loadPostgresParams(ctx, q, oid)
	if err != nil {
		return routineDetail{}, err
	}
	detail := routineDetail{
		routineInfo: routineInfo{
			Schema:       schema,
			Name:         name,
			Type:         kind,
			Language:     language,
			ReturnType:   strings.TrimSpace(returnType),
			IdentityArgs: identityArgs,
		},
		Parameters: params,
		Definition: definition,
	}
	if kind == "procedure" {
		detail.ReturnType = ""
	}
	return detail, nil
}

func (e *Explorer) loadPostgresParams(ctx context.Context, q queryer, oid int64) ([]routineParam, error) {
	const query = `
SELECT COALESCE(p.parameter_name, ''),
       COALESCE(p.parameter_mode, ''),
       CASE
         WHEN p.data_type = 'USER-DEFINED' THEN COALESCE(p.udt_name, p.data_type)
         ELSE COALESCE(p.data_type, '')
       END,
       p.ordinal_position
FROM information_schema.parameters AS p
JOIN pg_catalog.pg_proc AS pr ON p.specific_name = pr.proname || '_' || pr.oid::text
JOIN pg_catalog.pg_namespace AS n ON n.oid = pr.pronamespace AND p.specific_schema = n.nspname
WHERE pr.oid = $1
ORDER BY p.ordinal_position`
	rows, err := e.query(ctx, q, query, oid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var params []routineParam
	for rows.Next() {
		var p routineParam
		if err := rows.Scan(&p.Name, &p.Mode, &p.DataType, &p.Position); err != nil {
			return nil, err
		}
		params = append(params, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if params == nil {
		params = []routineParam{}
	}
	return params, nil
}

func (e *Explorer) loadMySQLRoutine(ctx context.Context, q queryer, schema, name, kind string) (routineDetail, error) {
	const meta = `
SELECT COALESCE(external_language, 'SQL'),
       COALESCE(dtd_identifier, '')
FROM information_schema.routines
WHERE routine_schema = ?
  AND routine_name = ?
  AND routine_type = ?`
	routineType := "FUNCTION"
	if kind == "procedure" {
		routineType = "PROCEDURE"
	}
	var language, returnType string
	err := q.QueryRowContext(ctx, meta, schema, name, routineType).Scan(&language, &returnType)
	if err == sql.ErrNoRows {
		return routineDetail{}, errNotFound
	}
	if err != nil {
		return routineDetail{}, err
	}

	showSQL := fmt.Sprintf("SHOW CREATE %s %s", routineType, e.dialect.Qualify(schema, name))
	rows, err := e.query(ctx, q, showSQL)
	if err != nil {
		return routineDetail{}, err
	}
	defer func() { _ = rows.Close() }()
	cols, err := rows.Columns()
	if err != nil {
		return routineDetail{}, err
	}
	if !rows.Next() {
		return routineDetail{}, errNotFound
	}
	raw := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range raw {
		ptrs[i] = &raw[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return routineDetail{}, err
	}
	if err := rows.Err(); err != nil {
		return routineDetail{}, err
	}
	definition := ""
	for i, col := range cols {
		lower := strings.ToLower(col)
		if strings.Contains(lower, "create") {
			definition = stringifyCell(raw[i])
			break
		}
	}
	if definition == "" && len(raw) > 1 {
		definition = stringifyCell(raw[1])
	}

	params, err := e.loadMySQLParams(ctx, q, schema, name, routineType)
	if err != nil {
		return routineDetail{}, err
	}
	detail := routineDetail{
		routineInfo: routineInfo{
			Schema:     schema,
			Name:       name,
			Type:       kind,
			Language:   language,
			ReturnType: strings.TrimSpace(returnType),
		},
		Parameters: params,
		Definition: definition,
	}
	if kind == "procedure" {
		detail.ReturnType = ""
	}
	return detail, nil
}

func (e *Explorer) loadMySQLParams(ctx context.Context, q queryer, schema, name, routineType string) ([]routineParam, error) {
	const query = `
SELECT COALESCE(parameter_name, ''),
       COALESCE(parameter_mode, ''),
       COALESCE(dtd_identifier, data_type, ''),
       ordinal_position
FROM information_schema.parameters
WHERE specific_schema = ?
  AND specific_name = ?
  AND routine_type = ?
  AND parameter_mode IS NOT NULL
ORDER BY ordinal_position`
	rows, err := e.query(ctx, q, query, schema, name, routineType)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var params []routineParam
	for rows.Next() {
		var p routineParam
		if err := rows.Scan(&p.Name, &p.Mode, &p.DataType, &p.Position); err != nil {
			return nil, err
		}
		params = append(params, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if params == nil {
		params = []routineParam{}
	}
	return params, nil
}

func stringifyCell(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []byte:
		return string(t)
	case sql.NullString:
		if t.Valid {
			return t.String
		}
		return ""
	default:
		return fmt.Sprint(t)
	}
}
