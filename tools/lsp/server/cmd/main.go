package main

import (
	"os"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	lspserver "github.com/kro-run/kro/tools/lsp/server"
	"github.com/tliron/glsp/server"
	"go.uber.org/zap"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func getKubernetesConfig(log logr.Logger) *rest.Config {
	if config, err := rest.InClusterConfig(); err == nil {
		return config
	}
	kubeconfigPath := clientcmd.RecommendedHomeFile
	if config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath); err == nil {
		return config
	}
	return nil
}

func main() {
	zapLogger, _ := zap.NewDevelopment()
	log := zapr.NewLogger(zapLogger)

	clientConfig := getKubernetesConfig(log)
	kroServer := lspserver.NewKroServer(log, clientConfig, nil)
	handler := kroServer.CreateHandler()
	lspServer := server.NewServer(handler, "kro-language-server", false)
	kroServer.SetServer(lspServer)

	if err := lspServer.RunStdio(); err != nil {
		log.Error(err, "Error running LSP server")
		os.Exit(1)
	}
}
