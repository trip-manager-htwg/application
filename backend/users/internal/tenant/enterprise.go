package tenant

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
)

type GitHubProvisioner struct {
	Token      string
	Repo       string
	Branch     string
	GCPProject string
}

func NewGitHubProvisioner(token, repo, branch, gcpProject string) *GitHubProvisioner {
	return &GitHubProvisioner{
		Token:      token,
		Repo:       repo,
		Branch:     branch,
		GCPProject: gcpProject,
	}
}

func (g *GitHubProvisioner) createGCPSecret(ctx context.Context, secretName, value string) error {
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create secret manager client: %w", err)
	}
	defer func(client *secretmanager.Client) {
		err := client.Close()
		if err != nil {
			log.Printf("failed to close secret manager client: %v", err)
		}
	}(client)

	// Secret erstellen
	_, err = client.CreateSecret(ctx, &secretmanagerpb.CreateSecretRequest{
		Parent:   fmt.Sprintf("projects/%s", g.GCPProject),
		SecretId: secretName,
		Secret: &secretmanagerpb.Secret{
			Replication: &secretmanagerpb.Replication{
				Replication: &secretmanagerpb.Replication_Automatic_{
					Automatic: &secretmanagerpb.Replication_Automatic{},
				},
			},
		},
	})
	// Ignoriere Fehler wenn Secret bereits existiert
	if err != nil && !isAlreadyExists(err) {
		return fmt.Errorf("failed to create secret: %w", err)
	}

	// Secret Version hinzufügen
	_, err = client.AddSecretVersion(ctx, &secretmanagerpb.AddSecretVersionRequest{
		Parent: fmt.Sprintf("projects/%s/secrets/%s", g.GCPProject, secretName),
		Payload: &secretmanagerpb.SecretPayload{
			Data: []byte(value),
		},
	})
	return err
}

func isAlreadyExists(err error) bool {
	return err != nil && (contains(err.Error(), "AlreadyExists") || contains(err.Error(), "already exists"))
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsRune(s, substr))
}

func containsRune(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func generateDBPassword() string {
	return fmt.Sprintf("ent-%d", time.Now().UnixNano())
}

func (g *GitHubProvisioner) ProvisionEnterpriseTenant(ctx context.Context, tenantSlug, dbPassword string) error {
	secretName := fmt.Sprintf("enterprise-%s-db-password", tenantSlug)

	// 1. Passwort in GCP Secret Manager speichern
	if err := g.createGCPSecret(ctx, secretName, dbPassword); err != nil {
		return fmt.Errorf("failed to create GCP secret: %w", err)
	}

	// 2. Namespace
	namespace := fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: trip-manager-%s
`, tenantSlug)

	// 3. ExternalSecret
	externalSecret := fmt.Sprintf(`apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: trips-postgres-%s-secret
  namespace: trip-manager-%s
spec:
  refreshInterval: 1h
  secretStoreRef:
    kind: ClusterSecretStore
    name: gcp-secret-store
  target:
    name: trips-postgres-%s-secret
  data:
    - secretKey: POSTGRES_PASSWORD
      remoteRef:
        key: %s
`, tenantSlug, tenantSlug, tenantSlug, secretName)

	// 4. StatefulSet
	statefulSet := fmt.Sprintf(`apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: trips-postgres-%s
  namespace: trip-manager-%s
spec:
  serviceName: trips-postgres-%s
  replicas: 1
  selector:
    matchLabels:
      app: trips-postgres
      tenant: %s
  template:
    metadata:
      labels:
        app: trips-postgres
        tenant: %s
    spec:
      containers:
        - name: postgres
          image: postgres:16-alpine
          args: ["-c", "hba_file=/etc/postgresql/pg_hba.conf"]
          env:
            - name: POSTGRES_DB
              value: trips
            - name: POSTGRES_USER
              value: trips_enterprise
            - name: POSTGRES_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: trips-postgres-%s-secret
                  key: POSTGRES_PASSWORD
          ports:
            - containerPort: 5432
          volumeMounts:
            - name: data
              mountPath: /var/lib/postgresql/data
              subPath: postgres
            - name: config
              mountPath: /etc/postgresql
          resources:
            requests:
              cpu: 100m
              memory: 256Mi
            limits:
              cpu: 500m
              memory: 512Mi
      volumes:
        - name: config
          configMap:
            name: trips-postgres-%s-config
  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes: ["ReadWriteOnce"]
        storageClassName: standard-rwo
        resources:
          requests:
            storage: 5Gi
`, tenantSlug, tenantSlug, tenantSlug, tenantSlug, tenantSlug, tenantSlug, tenantSlug)

	// 5. Service
	service := fmt.Sprintf(`apiVersion: v1
kind: Service
metadata:
  name: trips-postgres-%s
  namespace: trip-manager-%s
spec:
  type: ClusterIP
  ports:
    - port: 5432
      targetPort: 5432
  selector:
    app: trips-postgres
    tenant: %s
`, tenantSlug, tenantSlug, tenantSlug)

	// 6. ConfigMap für pg_hba.conf
	configMap := fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: trips-postgres-%s-config
  namespace: trip-manager-%s
data:
  pg_hba.conf: |
    local   all             all                                     trust
    host    all             all             127.0.0.1/32            trust
    host    all             all             ::1/128                 trust
    local   replication     all                                     trust
    host    replication     all             127.0.0.1/32            trust
    host    replication     all             ::1/128                 trust
    host    all             all             all                     md5
`, tenantSlug, tenantSlug)

	// Alle Dateien committen
	files := map[string]string{
		fmt.Sprintf("gitops/enterprise/%s/namespace.yaml", tenantSlug):      namespace,
		fmt.Sprintf("gitops/enterprise/%s/configmap.yaml", tenantSlug):      configMap,
		fmt.Sprintf("gitops/enterprise/%s/externalsecret.yaml", tenantSlug): externalSecret,
		fmt.Sprintf("gitops/enterprise/%s/statefulset.yaml", tenantSlug):    statefulSet,
		fmt.Sprintf("gitops/enterprise/%s/service.yaml", tenantSlug):        service,
	}

	for path, content := range files {
		if err := g.createOrUpdateFile(ctx, path, content,
			fmt.Sprintf("feat: provision enterprise tenant %s", tenantSlug)); err != nil {
			return fmt.Errorf("failed to create %s: %w", path, err)
		}
	}

	return nil
}

func (g *GitHubProvisioner) createOrUpdateFile(ctx context.Context, path, content, message string) error {
	existingSHA := ""
	url := fmt.Sprintf("https://api.github.com/repos/%s/contents/%s?ref=%s", g.Repo, path, g.Branch)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+g.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	if resp, err := http.DefaultClient.Do(req); err == nil {
		defer func(Body io.ReadCloser) {
			err := Body.Close()
			if err != nil {
				log.Printf("failed to close response body: %v", err)
			}
		}(resp.Body)
		if resp.StatusCode == 200 {
			var existing struct {
				SHA string `json:"sha"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&existing); err != nil {
				return err
			}
			existingSHA = existing.SHA
		}
	}

	body := map[string]interface{}{
		"message": message,
		"content": base64.StdEncoding.EncodeToString([]byte(content)),
		"branch":  g.Branch,
	}
	if existingSHA != "" {
		body["sha"] = existingSHA
	}

	bodyJSON, _ := json.Marshal(body)
	putURL := fmt.Sprintf("https://api.github.com/repos/%s/contents/%s", g.Repo, path)
	putReq, _ := http.NewRequestWithContext(ctx, "PUT", putURL, bytes.NewReader(bodyJSON))
	putReq.Header.Set("Authorization", "Bearer "+g.Token)
	putReq.Header.Set("Accept", "application/vnd.github+json")
	putReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(putReq)
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Printf("failed to close response body: %v", err)
		}
	}(resp.Body)

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github API error: %d - %s", resp.StatusCode, string(body))
	}
	return nil
}

func (g *GitHubProvisioner) DeprovisionEnterpriseTenant(ctx context.Context, tenantSlug string) error {
	files, err := g.listFiles(ctx, fmt.Sprintf("gitops/enterprise/%s", tenantSlug))
	if err != nil {
		return err
	}
	for _, file := range files {
		if err := g.deleteFile(ctx, file.Path, file.SHA,
			fmt.Sprintf("feat: deprovision enterprise tenant %s", tenantSlug)); err != nil {
			return err
		}
	}
	return nil
}

type githubFile struct {
	Path string
	SHA  string
}

func (g *GitHubProvisioner) listFiles(ctx context.Context, path string) ([]githubFile, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/contents/%s?ref=%s", g.Repo, path, g.Branch)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+g.Token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Printf("failed to close response body: %v", err)
		}
	}(resp.Body)

	if resp.StatusCode == 404 {
		return nil, nil
	}

	var items []struct {
		Path string `json:"path"`
		SHA  string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, err
	}

	var files []githubFile
	for _, item := range items {
		files = append(files, githubFile{Path: item.Path, SHA: item.SHA})
	}
	return files, nil
}

func (g *GitHubProvisioner) deleteFile(ctx context.Context, path, sha, message string) error {
	body := map[string]interface{}{
		"message": message,
		"sha":     sha,
		"branch":  g.Branch,
	}
	bodyJSON, _ := json.Marshal(body)
	url := fmt.Sprintf("https://api.github.com/repos/%s/contents/%s", g.Repo, path)
	req, _ := http.NewRequestWithContext(ctx, "DELETE", url, bytes.NewReader(bodyJSON))
	req.Header.Set("Authorization", "Bearer "+g.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Printf("failed to close response body: %v", err)
		}
	}(resp.Body)
	return nil
}
