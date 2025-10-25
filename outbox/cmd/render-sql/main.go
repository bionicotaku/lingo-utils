package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

type renderOptions struct {
	Schema string
}

func main() {
	var (
		schema      = flag.String("schema", "", "目标 PostgreSQL schema 名称（必填）")
		templateDir = flag.String("template-dir", "", "模板目录（默认：../schema/tmpl）")
		ddlOut      = flag.String("ddl-out", "", "生成的迁移 SQL 输出路径（必填）")
		sqlcOut     = flag.String("sqlc-out", "", "生成的 sqlc schema 输出路径（必填）")
	)
	flag.Parse()

	if *schema == "" {
		exitWithErr("missing required flag: -schema")
	}
	if *ddlOut == "" {
		exitWithErr("missing required flag: -ddl-out")
	}
	if *sqlcOut == "" {
		exitWithErr("missing required flag: -sqlc-out")
	}

	dir := *templateDir
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			exitWithErr("get working directory: %v", err)
		}
		dir = filepath.Join(cwd, "outbox", "schema", "tmpl")
	}

	opts := renderOptions{Schema: *schema}

	if err := renderFile(filepath.Join(dir, "outbox_inbox_ddl.sql.tmpl"), *ddlOut, opts); err != nil {
		exitWithErr("render ddl template: %v", err)
	}
	if err := renderFile(filepath.Join(dir, "outbox_inbox_sqlc_schema.sql.tmpl"), *sqlcOut, opts); err != nil {
		exitWithErr("render sqlc template: %v", err)
	}
}

func renderFile(tmplPath, outputPath string, opts renderOptions) error {
	tmplBytes, err := os.ReadFile(tmplPath)
	if err != nil {
		return fmt.Errorf("read template %s: %w", tmplPath, err)
	}

	tmpl, err := template.New(filepath.Base(tmplPath)).Parse(string(tmplBytes))
	if err != nil {
		return fmt.Errorf("parse template %s: %w", tmplPath, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, opts); err != nil {
		return fmt.Errorf("execute template %s: %w", tmplPath, err)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("ensure output dir: %w", err)
	}

	if err := os.WriteFile(outputPath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write output %s: %w", outputPath, err)
	}
	return nil
}

func exitWithErr(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "render-sql: "+format+"\n", args...)
	os.Exit(1)
}
