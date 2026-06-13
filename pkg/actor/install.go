package actor

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/hybridgroup/yzma/pkg/download"
)

func EnsureInstall(libPath string, processor string, updateInstall bool, installVersion string) error {
	if _, err := os.Stat(libPath); os.IsNotExist(err) {
		if err := os.Mkdir(libPath, 0755); err != nil {
			return err
		}
	}

	if !download.AlreadyInstalled(libPath) || updateInstall {
		var version string
		if installVersion != "" {
			version = installVersion
		} else {
			var err error
			version, err = download.LlamaLatestVersion()
			if err != nil {
				return err
			}
		}

		if processor == "" {
			processor = "cpu"
			if cudaInstalled, cudaVersion := download.HasCUDA(); cudaInstalled {
				log.Printf("CUDA detected (version %s), using CUDA build\n", cudaVersion)
				processor = "cuda"
			}
		}

		log.Printf("installing llama.cpp version %s to %s\n", version, libPath)
		if err := download.GetWithContext(context.Background(), runtime.GOARCH, "trixie", processor, version, libPath, download.ProgressTracker); err != nil {
			return err
		}
		log.Println("yzma installed successfully.")
	}

	return nil
}

func EnsureModel(modelUrl string, modelsPath string) error {
	if len(modelUrl) == 0 {
		return fmt.Errorf("model URL is required")
	}

	modelName := filepath.Base(modelUrl)
	modelFilePath := filepath.Join(modelsPath, modelName)

	if _, err := os.Stat(modelFilePath); os.IsNotExist(err) {
		log.Printf("installing model from %s to %s\n", modelUrl, modelFilePath)
		if err := download.GetModelWithContext(context.Background(), modelUrl, modelsPath, download.ProgressTracker); err != nil {
			log.Printf("failed to download model: %s\n", err.Error())
			return err
		}

		log.Println("model downloaded successfully.")
	}

	return nil
}
