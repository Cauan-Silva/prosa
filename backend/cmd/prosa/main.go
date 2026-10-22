// Comando prosa: o servidor de chat (API, tempo real e voz) num binário só.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Cauan-Silva/prosa/internal/arquivos"
	"github.com/Cauan-Silva/prosa/internal/auth"
	"github.com/Cauan-Silva/prosa/internal/banco"
	"github.com/Cauan-Silva/prosa/internal/config"
	"github.com/Cauan-Silva/prosa/internal/eventos"
	"github.com/Cauan-Silva/prosa/internal/mensagens"
	"github.com/Cauan-Silva/prosa/internal/presenca"
	"github.com/Cauan-Silva/prosa/internal/servidor"
	"github.com/Cauan-Silva/prosa/internal/voz"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	if err := rodar(); err != nil {
		slog.Error("o servidor parou", "erro", err)
		os.Exit(1)
	}
}

func rodar() error {
	cfg, err := config.Ler()
	if err != nil {
		return err
	}
	ctx, parar := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer parar()

	db, err := banco.Abrir(ctx, cfg.BancoURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := banco.Migrar(ctx, db); err != nil {
		return err
	}

	var armazem arquivos.Armazem
	if cfg.TemS3() {
		armazem = arquivos.NovoS3(cfg.S3Endpoint, cfg.S3Regiao, cfg.S3Bucket, cfg.S3Chave, cfg.S3Segredo)
		slog.Info("anexos no S3", "endpoint", cfg.S3Endpoint, "bucket", cfg.S3Bucket)
	} else {
		if armazem, err = arquivos.NovaPasta(cfg.PastaLocal); err != nil {
			return err
		}
		slog.Warn("anexos numa pasta local (somem se o servidor for recriado); configure S3_* em produção", "pasta", cfg.PastaLocal)
	}

	hub := eventos.NovoHub()
	var (
		publicador eventos.Publicador = eventos.Local{Hub: hub}
		registro   presenca.Registro
		limite     mensagens.Limitador
		coord      voz.Coordenador
	)
	if cfg.RedisURL != "" {
		opcoes, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			return errors.New("REDIS_URL inválida: " + err.Error())
		}
		cliente := redis.NewClient(opcoes)
		defer cliente.Close()
		if err := cliente.Ping(ctx).Err(); err != nil {
			return errors.New("não foi possível falar com o Redis: " + err.Error())
		}
		publicador = eventos.NovoRedis(ctx, cliente, hub)
		registro = presenca.NovoRedis(ctx, cliente, cfg.Instancia)
		limite = mensagens.LimiteRedis{Cliente: cliente, Janela: 5 * time.Second, Maximo: 8}
		coord = voz.Redis{Cliente: cliente, Instancia: cfg.Instancia}
		if cfg.EnderecoPublico == "" {
			slog.Warn("com várias instâncias, defina ENDERECO_PUBLICO em cada uma para as salas de voz não se dividirem")
		}
		slog.Info("várias instâncias: eventos e presença pelo Redis")
	} else {
		slog.Info("uma instância só: eventos e presença em memória (defina REDIS_URL para rodar várias)")
	}

	emissor := auth.NovoEmissor(cfg.SegredoJWT)
	var cloudflare *voz.TurnCloudflare
	if cfg.CFTurnID != "" && cfg.CFTurnToken != "" {
		cloudflare = &voz.TurnCloudflare{ChaveID: cfg.CFTurnID, Token: cfg.CFTurnToken}
		slog.Info("voz: usando o TURN do Cloudflare")
	}
	sfu, err := voz.Novo(voz.Opcoes{
		DB: db, Emissor: emissor, Hub: hub, Publicador: publicador, Coordenador: coord,
		Origens: cfg.OrigensWS, Endereco: cfg.EnderecoPublico, Instancia: cfg.Instancia,
		TurnURLs: cfg.TurnURLs, TurnUsuario: cfg.TurnUsuario, TurnSenha: cfg.TurnSenha, Cloudflare: cloudflare,
		PortaUDP: cfg.PortaUDP, IPPublico: cfg.IPPublico, MaxDireto: cfg.MaxDireto,
	})
	if err != nil {
		return err
	}
	go sfu.Manter(ctx)
	if len(cfg.TurnURLs) == 0 && cloudflare == nil {
		slog.Warn("voz sem TURN: funciona na mesma rede ou com UDP liberado; em hospedagens sem UDP (ex.: Render), configure TURN_URLS")
	}

	rotas, _ := servidor.Novo(servidor.Opcoes{
		Voz:            sfu,
		DB:             db,
		Emissor:        auth.NovoEmissor(cfg.SegredoJWT),
		Publicador:     publicador,
		Presenca:       registro,
		Limitador:      limite,
		Hub:            hub,
		OrigensWS:      cfg.OrigensWS,
		Armazem:        armazem,
		MaxAnexoMB:     cfg.MaxAnexoMB,
		Instancia:      cfg.Instancia,
		LoginPorMinuto: cfg.LoginPorMinuto,
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Porta,
		Handler:           rotas,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	erro := make(chan error, 1)
	go func() {
		slog.Info("Prosa no ar", "porta", cfg.Porta, "instancia", cfg.Instancia)
		erro <- srv.ListenAndServe()
	}()

	select {
	case err := <-erro:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		slog.Info("desligando…")
	}
	desligar, cancelar := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelar()
	return srv.Shutdown(desligar)
}
