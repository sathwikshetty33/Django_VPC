package services

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/nacl/box"
	"golang.org/x/crypto/ssh"
)

type GitHubSecret struct {
	EncryptedValue string `json:"encrypted_value"`
	KeyID          string `json:"key_id"`
}

type GitHubPublicKey struct {
	KeyID string `json:"key_id"`
	Key   string `json:"key"`
}

func extractRepoName(repoURL string) (string, error) {
	parsedURL, err := url.Parse(repoURL)
	if err != nil {
		return "", err
	}

	path := strings.TrimPrefix(parsedURL.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid repository URL format")
	}
	
	return parts[len(parts)-1], nil
}

func (ds *DeploymentService) extractOwnerAndRepo(repoURL string) (string, string, error) {
	parsedURL, err := url.Parse(repoURL)
	if err != nil {
		return "", "", err
	}

	path := strings.TrimPrefix(parsedURL.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid repository URL format")
	}
	
	return parts[0], parts[1], nil
}

func (ds *DeploymentService) generateSSHKeyPair() (string, string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate private key: %v", err)
	}

	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}
	privateKeyBytes := pem.EncodeToMemory(privateKeyPEM)

	publicKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate public key: %v", err)
	}

	publicKeyBytes := ssh.MarshalAuthorizedKey(publicKey)

	return string(privateKeyBytes), string(publicKeyBytes), nil
}

func (ds *DeploymentService) getGitHubPublicKey(owner, repo, token string) (*GitHubPublicKey, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/secrets/public-key", owner, repo)
	
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	
	req.Header.Set("Authorization", fmt.Sprintf("token %s", token))
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch public key: %v", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API error (status %d): %s", resp.StatusCode, string(body))
	}
	
	var publicKey GitHubPublicKey
	if err := json.NewDecoder(resp.Body).Decode(&publicKey); err != nil {
		return nil, fmt.Errorf("failed to decode public key response: %v", err)
	}
	
	return &publicKey, nil
}

func (ds *DeploymentService) encryptSecret(secretValue, publicKeyStr string) (string, error) {
	publicKeyBytes, err := base64.StdEncoding.DecodeString(publicKeyStr)
	if err != nil {
		return "", fmt.Errorf("failed to decode public key: %v", err)
	}

	if len(publicKeyBytes) != 32 {
		return "", fmt.Errorf("invalid public key length: expected 32 bytes, got %d", len(publicKeyBytes))
	}

	recipientKey := new([32]byte)
	copy(recipientKey[:], publicKeyBytes)

	pubKey, privKey, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("failed to generate ephemeral key pair: %v", err)
	}

	nonceHash, err := blake2b.New(24, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create nonce hash: %v", err)
	}

	nonceHash.Write(pubKey[:])
	nonceHash.Write(recipientKey[:])

	nonce := new([24]byte)
	copy(nonce[:], nonceHash.Sum(nil))


	encrypted := box.Seal(pubKey[:], []byte(secretValue), nonce, recipientKey, privKey)

	return base64.StdEncoding.EncodeToString(encrypted), nil
}

func (ds *DeploymentService) setGitHubSecret(owner, repo, secretName, secretValue, token string, publicKey *GitHubPublicKey) error {
	encryptedValue, err := ds.encryptSecret(secretValue, publicKey.Key)
	if err != nil {
		return fmt.Errorf("failed to encrypt secret: %v", err)
	}
	
	secret := GitHubSecret{
		EncryptedValue: encryptedValue,
		KeyID:          publicKey.KeyID,
	}
	
	secretJSON, err := json.Marshal(secret)
	if err != nil {
		return fmt.Errorf("failed to marshal secret: %v", err)
	}
	
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/secrets/%s", owner, repo, secretName)
	
	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(secretJSON))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}
	
	req.Header.Set("Authorization", fmt.Sprintf("token %s", token))
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Content-Type", "application/json")
	
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to set secret: %v", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GitHub API error setting secret %s (status %d): %s", secretName, resp.StatusCode, string(body))
	}
	
	log.Printf("Successfully set GitHub secret: %s", secretName)
	return nil
}

func (ds *DeploymentService) setupGitHubSecrets(req *DeploymentRequest, privateKey string) error {
	if req.GithubToken == "" {
		return fmt.Errorf("GitHub token is required for setting up secrets")
	}
	
	owner, repo, err := ds.extractOwnerAndRepo(req.RepoURL)
	if err != nil {
		return fmt.Errorf("failed to extract owner and repo from URL: %v", err)
	}
	
	publicKey, err := ds.getGitHubPublicKey(owner, repo, req.GithubToken)
	if err != nil {
		return fmt.Errorf("failed to get GitHub public key: %v", err)
	}
	
	if err := ds.setGitHubSecret(owner, repo, "SSH_PRIVATE_KEY", privateKey, req.GithubToken, publicKey); err != nil {
		return fmt.Errorf("failed to set SSH private key secret: %v", err)
	}
	
	for key, value := range req.EnvVariables {
		secretName := fmt.Sprintf("ENV_%s", strings.ToUpper(key))
		if err := ds.setGitHubSecret(owner, repo, secretName, value, req.GithubToken, publicKey); err != nil {
			log.Printf("Warning: failed to set environment variable secret %s: %v", secretName, err)
		}
	}
	
	log.Printf("Successfully configured GitHub secrets for repository %s/%s", owner, repo)
	return nil
}

func (ds *DeploymentService) createGitHubActionsWorkflow(workDir string, req *DeploymentRequest, publicIP string) error {
	workflowDir := filepath.Join(workDir, "github-actions")
	if err := os.MkdirAll(workflowDir, 0755); err != nil {
		return fmt.Errorf("failed to create workflow directory: %v", err)
	}

	workflowContent := fmt.Sprintf(`name: Auto Deploy Django Application

on:
  push:
    branches: [ main, master ]
  pull_request:
    branches: [ main, master ]
    types: [closed]

jobs:
  deploy:
    if: github.event_name == 'push' || (github.event_name == 'pull_request' && github.event.pull_request.merged == true)
    runs-on: ubuntu-latest
    
    steps:
    - name: Deploy to server
      uses: appleboy/ssh-action@v1.0.3
      with:
        host: %s
        username: azureuser
        key: ${{ secrets.SSH_PRIVATE_KEY }}
        script: |
          echo "Starting auto-deployment..."
          
          # Navigate to app directory
          cd /home/azureuser/app
          
          # Stop the Django server
          sudo supervisorctl stop django-server || true
          
          # Pull latest changes
          git pull origin main || git pull origin master
          
          # Activate virtual environment and install/update dependencies
          source venv/bin/activate
          
          # Install any new dependencies
          if [ -f requirements.txt ]; then
            pip install -r requirements.txt --no-cache-dir
          elif [ -f */requirements.txt ]; then
            pip install -r */requirements.txt --no-cache-dir
          fi
          
          # Set environment variables from GitHub secrets
%s
          
          # Find Django project directory
          MANAGE_PY=$(find . -name "manage.py" | head -1)
          if [ -n "$MANAGE_PY" ]; then
            PROJECT_DIR=$(dirname "$MANAGE_PY")
            cd "$PROJECT_DIR"
            
            # Run migrations
            python manage.py migrate --noinput
            
            # Collect static files
            python manage.py collectstatic --noinput || true
            
            # Run additional commands if any
%s
          fi
          
          # Start the Django server
          sudo supervisorctl start django-server
          
          # Wait a moment and check status
          sleep 5
          sudo supervisorctl status django-server
          
          echo "Auto-deployment completed!"
      env:
%s
`, publicIP, ds.generateEnvExports(req.EnvVariables), ds.generateAdditionalCommands(req.AdditionalCommands), ds.generateEnvSecrets(req.EnvVariables))

	workflowPath := filepath.Join(workflowDir, "deploy.yml")
	if err := os.WriteFile(workflowPath, []byte(workflowContent), 0644); err != nil {
		return fmt.Errorf("failed to write workflow file: %v", err)
	}

	return nil
}

func (ds *DeploymentService) generateEnvExports(envVars map[string]string) string {
	var exports strings.Builder
	for key, _ := range envVars {

		exports.WriteString(fmt.Sprintf("          export %s=\"$%s\"\n", key, key))
	}
	return exports.String()
}

func (ds *DeploymentService) generateEnvSecrets(envVars map[string]string) string {
	var secrets strings.Builder
	for key, _ := range envVars {
		secretName := fmt.Sprintf("ENV_%s", strings.ToUpper(key))
		secrets.WriteString(fmt.Sprintf("        %s: ${{ secrets.%s }}\n", key, secretName))
	}
	return secrets.String()
}

func (ds *DeploymentService) generateAdditionalCommands(additionalCommands []string) string {
	if len(additionalCommands) == 0 {
		return ""
	}

	var commands strings.Builder
	commands.WriteString("            # Additional commands\n")
	for _, cmd := range additionalCommands {
		commands.WriteString(fmt.Sprintf("            %s\n", cmd))
	}
	return commands.String()
}

func (ds *DeploymentService) setupGitHubActionsOnServer(ansibleDir string, req *DeploymentRequest, publicIP string, terraformDir string) error {
	if !req.AutoDeploy {
		return nil 
	}

	privateKeyPath := filepath.Join(terraformDir, "azure_vm_key")
	publicKeyPath := filepath.Join(terraformDir, "azure_vm_key.pub")

	privateKeyBytes, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read existing private key from %s: %v", privateKeyPath, err)
	}
	privateKey := string(privateKeyBytes)

	publicKeyBytes, err := os.ReadFile(publicKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read existing public key from %s: %v", publicKeyPath, err)
	}
	publicKey := strings.TrimSpace(string(publicKeyBytes))

	if err := ds.setupGitHubSecrets(req, privateKey); err != nil {
		return fmt.Errorf("failed to setup GitHub secrets: %v", err)
	}

	workDir := filepath.Dir(ansibleDir)
	if err := ds.createGitHubActionsWorkflow(workDir, req, publicIP); err != nil {
		return fmt.Errorf("failed to create GitHub Actions workflow: %v", err)
	}

	additionalTasksPath := filepath.Join(ansibleDir, "github-actions-setup.yml")
	
	additionalTasksContent := fmt.Sprintf(`---
- name: Setup GitHub Actions Auto-Deploy
  hosts: django_servers
  become: yes
  vars:
    public_key: "%s"
    repo_url: "%s"
    github_token: "%s"
  tasks:
    - name: Add GitHub Actions public key to authorized_keys (already exists but ensuring)
      authorized_key:
        user: azureuser
        state: present
        key: "{{ public_key }}"
        comment: "GitHub Actions Deploy Key (same as Azure VM key)"
      become_user: azureuser

    - name: Create .github/workflows directory in repository
      file:
        path: /home/azureuser/app/.github/workflows
        state: directory
        owner: azureuser
        group: azureuser
        mode: '0755'
      become_user: azureuser

    - name: Copy GitHub Actions workflow to repository
      copy:
        src: ../github-actions/deploy.yml
        dest: /home/azureuser/app/.github/workflows/deploy.yml
        owner: azureuser
        group: azureuser
        mode: '0644'
      become_user: azureuser

    - name: Configure git for commits
      shell: |
        cd /home/azureuser/app
        git config user.name "Auto Deploy Bot"
        git config user.email "deploy@auto-deploy.local"
      become_user: azureuser

    - name: Add and commit GitHub Actions workflow
      shell: |
        cd /home/azureuser/app
        git add .github/workflows/deploy.yml
        git commit -m "Add auto-deployment GitHub Actions workflow" || echo "No changes to commit"
      become_user: azureuser
      ignore_errors: yes

    - name: Push GitHub Actions workflow to repository
      shell: |
        cd /home/azureuser/app
        git push https://{{ github_token }}@{{ repo_url | regex_replace('https://') }} || echo "Failed to push - may need manual intervention"
      become_user: azureuser
      ignore_errors: yes

    - name: Set up log rotation for deployment logs
      copy:
        content: |
          /home/azureuser/logs/*.log {
              daily
              missingok
              rotate 7
              compress
              notifempty
              create 644 azureuser azureuser
              postrotate
                  /usr/bin/supervisorctl reread > /dev/null 2>&1 || true
                  /usr/bin/supervisorctl update > /dev/null 2>&1 || true
              endscript
          }
        dest: /etc/logrotate.d/django-app
        mode: '0644'

    - name: Display setup completion message
      debug:
        msg: |
          ============================================
          GitHub Actions Auto-Deploy Setup Complete!
          ============================================
          
          ✅ SSH private key has been automatically added to GitHub secrets (reusing Terraform key)
          ✅ Environment variables have been added to GitHub secrets
          ✅ GitHub Actions workflow has been created and committed
          
          Your repository will now auto-deploy on every push to main/master branch!
          
          The same SSH key used for VM creation is now being used for GitHub Actions deployment.
          
          You can monitor deployments in the "Actions" tab of your GitHub repository.
          ============================================
`, strings.TrimSpace(publicKey), req.RepoURL, req.GithubToken)

	if err := os.WriteFile(additionalTasksPath, []byte(additionalTasksContent), 0644); err != nil {
		return fmt.Errorf("failed to write additional tasks file: %v", err)
	}

	log.Printf("GitHub Actions setup configured using existing SSH keys from Terraform")
	return nil
}

// func (ds *DeploymentService) escapeYAMLString(s string) string {
// 	// Replace any quotes with escaped quotes
// 	s = strings.ReplaceAll(s, "\"", "\\\"")
// 	return s
// }

// func (ds *DeploymentService) indentString(s, indent string) string {
// 	lines := strings.Split(strings.TrimSpace(s), "\n")
// 	var result []string
// 	for _, line := range lines {
// 		result = append(result, indent+line)
// 	}
// 	return strings.Join(result, "\n")
// }

func (ds *DeploymentService) runAdditionalAnsibleTasks(ansibleDir string) error {
	additionalTasksPath := filepath.Join(ansibleDir, "github-actions-setup.yml")
	if _, err := os.Stat(additionalTasksPath); os.IsNotExist(err) {
		return nil 
	}

	log.Println("Running GitHub Actions setup tasks...")
	cmd := exec.Command("ansible-playbook", "-i", "inventory.ini", "github-actions-setup.yml", "-v")
	cmd.Dir = ansibleDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}