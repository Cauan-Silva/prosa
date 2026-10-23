package mensagens

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/lib/pq"

	"github.com/Cauan-Silva/prosa/internal/acesso"
	"github.com/Cauan-Silva/prosa/internal/eventos"
	"github.com/Cauan-Silva/prosa/internal/permissoes"
)

// Conversas diretas (DM e grupo) são canais sem servidor: quem participa está em
// conversa_membros, as permissões são fixas e os eventos vão para cada participante.

// contextoDireto confere se a pessoa participa da conversa e monta as permissões dela.
// Numa DM com bloqueio (de qualquer lado) a conversa fica só para ler.
func (s *Servico) contextoDireto(ctx context.Context, canalID, usuarioID int64, tipo string) (*acesso.Contexto, error) {
	var participa, bloqueio bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM conversa_membros WHERE canal_id = $1 AND usuario_id = $2),
		       $3 = 'dm' AND EXISTS (
		           SELECT 1 FROM conversa_membros o JOIN bloqueios b
		             ON (b.quem_id = $2 AND b.bloqueado_id = o.usuario_id) OR (b.quem_id = o.usuario_id AND b.bloqueado_id = $2)
		           WHERE o.canal_id = $1 AND o.usuario_id <> $2)`, canalID, usuarioID, tipo).Scan(&participa, &bloqueio)
	if err != nil {
		return nil, err
	}
	if !participa {
		return nil, acesso.ErrCanalSumiu
	}
	perm := acesso.PermissoesDiretas
	if bloqueio {
		perm = permissoes.VerCanal | permissoes.LerHistorico
	}
	return acesso.Direto(canalID, usuarioID, perm), nil
}

func (s *Servico) membrosDaConversa(ctx context.Context, canalID int64) ([]int64, error) {
	var ids pq.Int64Array
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(array_agg(usuario_id), '{}') FROM conversa_membros WHERE canal_id = $1`, canalID).Scan(&ids)
	return ids, err
}

// publicarDireto manda o evento para o tópico pessoal de cada participante.
func (s *Servico) publicarDireto(ctx context.Context, e eventos.Evento) {
	ids, err := s.membrosDaConversa(ctx, e.CanalID)
	if err != nil {
		slog.Error("conversa: participantes", "erro", err)
		return
	}
	for _, id := range ids {
		if err := s.pub.Publicar(ctx, eventos.TopicoUsuario(id), e); err != nil {
			slog.Error("não foi possível publicar o evento", "tipo", e.Tipo, "erro", err)
		}
	}
}

// reabrirConversa: mensagem nova numa conversa fechada faz ela voltar para a lista.
func (s *Servico) reabrirConversa(ctx context.Context, canalID int64) {
	if _, err := s.db.ExecContext(ctx, `UPDATE conversa_membros SET aberta = TRUE WHERE canal_id = $1 AND NOT aberta`, canalID); err != nil {
		slog.Warn("conversa: reabrir", "erro", err)
	}
}

// nulo vira NULL no banco quando o id é 0 (anexos de conversas diretas não têm servidor).
func nulo(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: id != 0} }
