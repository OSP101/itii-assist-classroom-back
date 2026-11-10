package main

import (
	"context"
	"encoding/json"
	"flag"
	"gorm.io/gorm/logger"
	"itii-assist/config"
	"itii-assist/repositories"
	"itii-assist/utils"
	"log"
	"os"
)

func main() {
	apply := flag.Bool("apply", false, "atomically rotate ciphertext; the default is a read-only dry run")
	flag.Parse()
	if err := utils.ValidateTwoFactorRotationKey(); err != nil {
		log.Fatal(err)
	}
	config.ConnectDB()
	config.DB.Logger = logger.Default.LogMode(logger.Silent)
	result, err := repositories.RotateTwoFactorEncryptionKey(context.Background(), !*apply)
	if err != nil {
		log.Fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		log.Fatal(err)
	}
}
