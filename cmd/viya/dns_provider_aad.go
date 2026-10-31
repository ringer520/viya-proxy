package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"viya/internal/secretstore"
)

type encryptedProvider struct {
	id         int64
	nonce      []byte
	ciphertext []byte
}

// migrateDNSProviderAAD changes the product namespace used as AES-GCM
// associated data while preserving credentials created by older releases.
func migrateDNSProviderAAD(db *sql.DB, key []byte) error {
	rows, err := db.Query(`SELECT id,credentials_nonce,credentials_ciphertext FROM dns_providers ORDER BY id`)
	if err != nil {
		return err
	}
	providers := []encryptedProvider{}
	for rows.Next() {
		var item encryptedProvider
		if err := rows.Scan(&item.id, &item.nonce, &item.ciphertext); err != nil {
			rows.Close()
			return err
		}
		providers = append(providers, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(providers) == 0 {
		return nil
	}

	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range providers {
		newAAD := []byte(fmt.Sprintf("viya:dns-provider:%d:v1", item.id))
		if currentPlaintext, err := secretstore.Decrypt(key, item.nonce, item.ciphertext, newAAD); err == nil {
			for i := range currentPlaintext {
				currentPlaintext[i] = 0
			}
			continue
		}
		oldAAD := []byte(fmt.Sprintf("crpm:dns-provider:%d:v1", item.id))
		plaintext, err := secretstore.Decrypt(key, item.nonce, item.ciphertext, oldAAD)
		if err != nil {
			return errors.New("a DNS provider credential cannot be decrypted; check that the original Viya master key was preserved")
		}
		newNonce, newCiphertext, err := secretstore.Encrypt(key, plaintext, newAAD)
		for i := range plaintext {
			plaintext[i] = 0
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE dns_providers SET credentials_nonce=?,credentials_ciphertext=? WHERE id=?`, newNonce, newCiphertext, item.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
