// Package banco abre a conexão com o PostgreSQL e aplica as migrações.
package banco

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq" // driver do PostgreSQL
)

//go:embed migracoes/*.sql
var migracoes embed.FS

// Abrir conecta ao banco, espera ele responder e aplica as migrações pendentes.
func Abrir(ctx context.Context, url string) (*sql.DB, error) {
	db, err := sql.Open("postgres", url)
	if err != nil {
		return nil, err
	}
	// Plano gratuito do Neon: poucas conexões bastam, e ele fecha as paradas.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxIdleTime(4 * time.Minute)

	// O banco gratuito pode estar "dormindo": tenta por até 30 segundos.
	prazo, cancelar := context.WithTimeout(ctx, 30*time.Second)
	defer cancelar()
	for {
		if err = db.PingContext(prazo); err == nil {
			break
		}
		select {
		case <-prazo.Done():
			db.Close()
			return nil, fmt.Errorf("banco não respondeu: %w", err)
		case <-time.After(time.Second):
		}
	}

	if err := Migrar(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Migrar aplica, em ordem e cada uma numa transação, as migrações ainda não aplicadas.
// Os arquivos se chamam 001_nome.sql, 002_nome.sql...
// Várias instâncias subindo juntas não brigam: uma trava no PostgreSQL faz as outras
// esperarem a primeira terminar.
func Migrar(ctx context.Context, banco *sql.DB) error {
	db, err := banco.Conn(ctx)
	if err != nil {
		return err
	}
	defer db.Close()
	const trava = 7_303_101 // número qualquer, só precisa ser sempre o mesmo
	if _, err := db.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, trava); err != nil {
		return err
	}
	defer db.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, trava)

	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS migracoes (
		versao INT PRIMARY KEY,
		aplicada_em TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return err
	}

	arquivos, err := fs.Glob(migracoes, "migracoes/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(arquivos)

	for _, arquivo := range arquivos {
		nome := strings.TrimPrefix(arquivo, "migracoes/")
		versao, err := strconv.Atoi(strings.SplitN(nome, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migração com nome inválido: %s", nome)
		}
		var existe bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM migracoes WHERE versao = $1)`, versao).Scan(&existe); err != nil {
			return err
		}
		if existe {
			continue
		}
		conteudo, err := migracoes.ReadFile(arquivo)
		if err != nil {
			return err
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(conteudo)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migração %s: %w", nome, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO migracoes (versao) VALUES ($1)`, versao); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		slog.Info("migração aplicada", "arquivo", nome)
	}
	return nil
}
