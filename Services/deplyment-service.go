package services

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"os/exec"
	"sathwikshetty33/Django-vpc/Providers"
)

type DeploymentService struct{}

type DeploymentRequest struct {
	RepoURL           string            `json:"repo_url"`
	ReqPath           string            `json:"req_path"`
	ManagePath        string            `json:"manage_path"`
	GithubToken       string            `json:"github_token"`
	Username          string            `json:"username"`
	AdditionalCommands []string         `json:"additional_commands"`
	EnvVariables      map[string]string `json:"env_variables"`
}

func NewDeploymentService() *DeploymentService {
	return &DeploymentService{}
}

func (ds *DeploymentService) Deploy(req *DeploymentRequest) (string, error) {
	// Extract repo name from URL
	repoName, err := extractRepoName(req.RepoURL)
	if err != nil {
		return "", fmt.Errorf("failed to extract repo name: %v", err)
	}

	// Create base path: username/repo_name
	basePath := filepath.Join("deployments", req.Username, repoName)
	
	// Create timestamp-based directory
	timestamp := time.Now().Format("20060102-150405")
	workDir := filepath.Join(basePath, timestamp)
	
	log.Printf("Creating deployment directory: %s", workDir)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create work directory: %v", err)
	}

	// Defer cleanup
	defer func() {
		log.Printf("Cleaning up deployment directory: %s", workDir)
		if err := os.RemoveAll(workDir); err != nil {
			log.Printf("Warning: failed to cleanup directory %s: %v", workDir, err)
		}
	}()

	// Create Terraform directory
	terraformDir := filepath.Join(workDir, "terraform")
	if err := os.MkdirAll(terraformDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create terraform directory: %v", err)
	}

	// Create Ansible directory
	ansibleDir := filepath.Join(workDir, "ansible")
	if err := os.MkdirAll(ansibleDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create ansible directory: %v", err)
	}

	// Initialize Azure provider with upgraded VM size
	azure := providers.AzureProvider{
		ResourceGroup: fmt.Sprintf("%s-%s-rg", req.Username, repoName),
		Location:      "East US",
		VMSize:        "Standard_B4ms", // Upgraded for better performance
		VMName:        fmt.Sprintf("%s-%s-vm", req.Username, repoName),
	}

	// Generate and apply Terraform
	log.Println("Generating Terraform configuration...")
	if err := azure.GenerateTerraformConfig(terraformDir); err != nil {
		return "", fmt.Errorf("failed to generate terraform config: %v", err)
	}

	log.Println("Initializing Terraform...")
	if err := azure.InitTerraform(terraformDir); err != nil {
		return "", fmt.Errorf("failed to initialize terraform: %v", err)
	}

	log.Println("Applying Terraform...")
	if err := azure.ApplyTerraform(terraformDir); err != nil {
		return "", fmt.Errorf("failed to apply terraform: %v", err)
	}

	// Get public IP
	log.Println("Retrieving public IP...")
	publicIP, err := azure.GetTerraformOutput(terraformDir, "public_ip")
	if err != nil {
		return "", fmt.Errorf("failed to get public IP: %v", err)
	}

	publicIP = strings.TrimSpace(publicIP)
	if publicIP == "" {
		return "", fmt.Errorf("public IP is empty")
	}

	log.Printf("Retrieved public IP: %s", publicIP)

	// Create Ansible files
	if err := ds.createAnsibleFiles(ansibleDir, req, publicIP); err != nil {
		return "", fmt.Errorf("failed to create ansible files: %v", err)
	}

	// Wait for VM to be ready
	log.Println("Waiting for VM to be ready...")
	time.Sleep(60 * time.Second) // Increased wait time

	// Run Ansible playbook
	log.Println("Running Ansible playbook...")
	if err := ds.runAnsiblePlaybook(ansibleDir); err != nil {
		return "", fmt.Errorf("failed to run ansible playbook: %v", err)
	}

	log.Println("Deployment completed successfully!")
	return publicIP, nil
}

func (ds *DeploymentService) createAnsibleFiles(ansibleDir string, req *DeploymentRequest, publicIP string) error {
	// Create inventory file
	inventoryContent := fmt.Sprintf(`[django_servers]
%s ansible_user=azureuser ansible_password=P@ssw0rd1234! ansible_connection=ssh ansible_ssh_common_args='-o StrictHostKeyChecking=no'
`, publicIP)

	inventoryPath := filepath.Join(ansibleDir, "inventory.ini")
	if err := os.WriteFile(inventoryPath, []byte(inventoryContent), 0644); err != nil {
		return fmt.Errorf("failed to write inventory file: %v", err)
	}

	// Create playbook
	playbookContent := ds.generatePlaybook(req)
	playbookPath := filepath.Join(ansibleDir, "playbook.yml")
	if err := os.WriteFile(playbookPath, []byte(playbookContent), 0644); err != nil {
		return fmt.Errorf("failed to write playbook file: %v", err)
	}

	return nil
}

func (ds *DeploymentService) generatePlaybook(req *DeploymentRequest) string {
	var envVars strings.Builder
	for key, value := range req.EnvVariables {
		envVars.WriteString(fmt.Sprintf("      %s: \"%s\"\n", key, value))
	}

	// Build additional tasks with proper indentation and newlines
	var additionalTasks strings.Builder
	for _, cmd := range req.AdditionalCommands {
		additionalTasks.WriteString(fmt.Sprintf(`

    - name: Run additional command
      shell: |
        cd {{ django_project_path }}
        source {{ django_project_path }}/../venv/bin/activate
        %s
      args:
        executable: /bin/bash
      become_user: azureuser
      environment: "{{ env_vars }}"`, cmd))
	}

	reqPathRelative := req.ReqPath
	if strings.HasPrefix(req.ReqPath, "/") {
		reqPathRelative = strings.TrimPrefix(req.ReqPath, "/home/azureuser/app/")
	}
	
	// Build supervisor environment variables
	var supervisorEnvVars strings.Builder
	if len(req.EnvVariables) > 0 {
		for key, value := range req.EnvVariables {
			supervisorEnvVars.WriteString(fmt.Sprintf(",%s=\"%s\"", key, value))
		}
	}

	// Create the complete playbook template
	var playbookBuilder strings.Builder
	
	// Header section
	playbookBuilder.WriteString(`---
- name: Deploy Django Application with Gunicorn
  hosts: django_servers
  become: yes
  vars:
    repo_url: "` + req.RepoURL + `"
    req_path: "` + reqPathRelative + `"
    manage_path: "` + req.ManagePath + `"
    github_token: "` + req.GithubToken + `"
    env_vars:
` + envVars.String() + `
  tasks:
    - name: Update apt cache
      apt:
        update_cache: yes

    - name: Install required packages
      apt:
        name:
          - python3
          - python3-pip
          - python3-dev
          - python3-venv
          - git
          - nginx
          - supervisor
          - build-essential
          - libpq-dev
          - pkg-config
          - default-libmysqlclient-dev
        state: present

    - name: Create application directory
      file:
        path: /home/azureuser/app
        state: directory
        owner: azureuser
        group: azureuser
        mode: '0755'

    - name: Clone repository
      git:
        repo: "https://{{ github_token }}@{{ repo_url | regex_replace('https://') }}"
        dest: /home/azureuser/app
        force: yes
      become_user: azureuser

    - name: Set proper permissions for cloned repository
      file:
        path: /home/azureuser/app
        owner: azureuser
        group: azureuser
        recurse: yes

    - name: Make manage.py executable
      shell: find /home/azureuser/app -name "manage.py" -exec chmod +x {} \;
      become_user: azureuser

    - name: Create virtual environment
      command: python3 -m venv venv
      args:
        chdir: /home/azureuser/app
        creates: /home/azureuser/app/venv
      become_user: azureuser

    - name: Upgrade pip in virtual environment
      pip:
        name: pip
        state: latest
        virtualenv: /home/azureuser/app/venv
      become_user: azureuser

    - name: Debug - List cloned repository structure
      find:
        paths: /home/azureuser/app
        recurse: yes
        file_type: file
        patterns: "requirements.txt"
      register: requirements_files

    - name: Set requirements file path
      set_fact:
        actual_req_path: "{{ requirements_files.files[0].path if requirements_files.files | length > 0 else '/home/azureuser/app/' + req_path }}"

    - name: Install Python dependencies
      pip:
        requirements: "{{ actual_req_path }}"
        virtualenv: /home/azureuser/app/venv
        extra_args: --no-cache-dir
      become_user: azureuser

    - name: Install Gunicorn and other production packages
      pip:
        name:
          - gunicorn
          - psycopg2-binary
          - whitenoise
        virtualenv: /home/azureuser/app/venv
      become_user: azureuser

    - name: Find Django manage.py file
      find:
        paths: /home/azureuser/app
        recurse: yes
        patterns: "manage.py"
      register: manage_files

    - name: Set Django project path
      set_fact:
        django_project_path: "{{ manage_files.files[0].path | dirname if manage_files.files | length > 0 else '/home/azureuser/app' }}"

    - name: Extract Django project configuration and WSGI module
      shell: |
        cd {{ django_project_path }}
        source /home/azureuser/app/venv/bin/activate
        export PYTHONPATH="/home/azureuser/app:$PYTHONPATH"
        
        # Extract project name and settings from manage.py
        python3 -c "
        import os, sys, re
        
        with open('manage.py', 'r') as f:
            content = f.read()
        
        # Find settings module
        settings_match = re.search(r'[\"\\']([^\"\\'\s]+\.settings)[\"\\']', content)
        if settings_match:
            settings_module = settings_match.group(1)
            project_name = settings_module.split('.')[0]
        else:
            # Fallback
            project_name = os.path.basename('{{ django_project_path }}')
            settings_module = project_name + '.settings'
        
        # Find WSGI module by looking for wsgi.py files
        wsgi_files = []
        for root, dirs, files in os.walk('.'):
            for file in files:
                if file == 'wsgi.py':
                    wsgi_path = os.path.join(root, file)
                    # Convert to module path
                    module_path = wsgi_path.replace('/', '.').replace('.py', '').lstrip('./')
                    wsgi_files.append(module_path)
        
        # Use the first wsgi module found, or construct default
        wsgi_module = wsgi_files[0] if wsgi_files else project_name + '.wsgi'
        
        print(f'{project_name}|{settings_module}|{wsgi_module}')
        "
      register: django_config
      become_user: azureuser

    - name: Parse Django configuration
      set_fact:
        django_project_name: "{{ django_config.stdout.split('|')[0] if '|' in django_config.stdout else 'myproject' }}"
        django_settings_module: "{{ django_config.stdout.split('|')[1] if '|' in django_config.stdout else 'myproject.settings' }}"
        django_wsgi_module: "{{ django_config.stdout.split('|')[2] if django_config.stdout.split('|') | length > 2 else 'myproject.wsgi' }}"

    - name: Display detected Django configuration
      debug:
        msg: |
          Detected Django Configuration:
          - Project Name: {{ django_project_name }}
          - Settings Module: {{ django_settings_module }}
          - WSGI Module: {{ django_wsgi_module }}
          - Project Path: {{ django_project_path }}

    - name: Create .env file for environment variables
      copy:
        content: |
          {% for key, value in env_vars.items() %}
          {{ key }}={{ value }}
          {% endfor %}
        dest: /home/azureuser/app/.env
        owner: azureuser
        group: azureuser
        mode: '0644'

    - name: Create media and static directories
      file:
        path: "{{ item }}"
        state: directory
        owner: azureuser
        group: azureuser
        mode: '0755'
      loop:
        - "{{ django_project_path }}/static"
        - "{{ django_project_path }}/media"
        - "{{ django_project_path }}/staticfiles"

    - name: Create log directories and files with proper permissions
      file:
        path: "{{ item.path }}"
        state: "{{ item.state }}"
        owner: "{{ item.owner }}"
        group: "{{ item.group }}"
        mode: "{{ item.mode }}"
      loop:
        - { path: "/home/azureuser/logs", state: "directory", owner: "azureuser", group: "azureuser", mode: "0755" }
        - { path: "/home/azureuser/logs/gunicorn-access.log", state: "touch", owner: "azureuser", group: "azureuser", mode: "0644" }
        - { path: "/home/azureuser/logs/gunicorn-error.log", state: "touch", owner: "azureuser", group: "azureuser", mode: "0644" }
        - { path: "/home/azureuser/logs/django-gunicorn-stdout.log", state: "touch", owner: "azureuser", group: "azureuser", mode: "0644" }
        - { path: "/home/azureuser/logs/django-gunicorn-stderr.log", state: "touch", owner: "azureuser", group: "azureuser", mode: "0644" }

    - name: Test Django configuration and WSGI module
      shell: |
        cd {{ django_project_path }}
        source /home/azureuser/app/venv/bin/activate
        export DJANGO_SETTINGS_MODULE="{{ django_settings_module }}"
        export PYTHONPATH="/home/azureuser/app:$PYTHONPATH"
        
        echo "=== Testing Django Configuration ==="
        python3 manage.py check --deploy || python3 manage.py check
        
        echo "=== Testing WSGI Module Import ==="
        python3 -c "
        import sys
        sys.path.insert(0, '/home/azureuser/app')
        try:
            from {{ django_wsgi_module }} import application
            print('WSGI module {{ django_wsgi_module }} imported successfully')
            print('Application object:', application)
        except Exception as e:
            print('Error importing WSGI module {{ django_wsgi_module }}:', e)
            # Try alternative WSGI paths
            import os
            for root, dirs, files in os.walk('/home/azureuser/app'):
                for file in files:
                    if file == 'wsgi.py':
                        print('Found wsgi.py at:', os.path.join(root, file))
        "
      args:
        executable: /bin/bash
      become_user: azureuser
      environment: "{{ env_vars }}"
      register: django_test
      ignore_errors: yes

    - name: Show Django test results
      debug:
        var: django_test.stdout_lines

    - name: Run Django migrations
      shell: |
        cd {{ django_project_path }}
        source /home/azureuser/app/venv/bin/activate
        export DJANGO_SETTINGS_MODULE="{{ django_settings_module }}"
        export PYTHONPATH="/home/azureuser/app:$PYTHONPATH"
        python3 manage.py migrate --noinput
      args:
        executable: /bin/bash
      become_user: azureuser
      environment: "{{ env_vars }}"

    - name: Collect static files
      shell: |
        cd {{ django_project_path }}
        source /home/azureuser/app/venv/bin/activate
        export DJANGO_SETTINGS_MODULE="{{ django_settings_module }}"
        export PYTHONPATH="/home/azureuser/app:$PYTHONPATH"
        python3 manage.py collectstatic --noinput
      args:
        executable: /bin/bash
      become_user: azureuser
      environment: "{{ env_vars }}"
      ignore_errors: yes`)

	// Add additional tasks if any
	if len(req.AdditionalCommands) > 0 {
		playbookBuilder.WriteString(additionalTasks.String())
	}

	// Continue with Gunicorn configuration
	playbookBuilder.WriteString(`

    - name: Create Gunicorn configuration file
      copy:
        content: |
          bind = "0.0.0.0:8000"
          workers = 3
          worker_class = "sync"
          worker_connections = 1000
          timeout = 300
          keepalive = 2
          max_requests = 1000
          max_requests_jitter = 50
          preload_app = True
          reload = False
          daemon = False
          user = "azureuser"
          group = "azureuser"
          tmp_upload_dir = None
          secure_scheme_headers = {
              'X-FORWARDED-PROTO': 'https',
          }
          forwarded_allow_ips = '*'
          access_log_format = '%(h)s %(l)s %(u)s %(t)s "%(r)s" %(s)s %(b)s "%(f)s" "%(a)s" %(D)s'
          accesslog = "/home/azureuser/logs/gunicorn-access.log"
          errorlog = "/home/azureuser/logs/gunicorn-error.log"
          loglevel = "info"
        dest: /home/azureuser/app/gunicorn.conf.py
        owner: azureuser
        group: azureuser
        mode: '0644'

    - name: Create Gunicorn startup script for testing
      copy:
        content: |
          #!/bin/bash
          cd {{ django_project_path }}
          source /home/azureuser/app/venv/bin/activate
          export DJANGO_SETTINGS_MODULE="{{ django_settings_module }}"
          export PYTHONPATH="/home/azureuser/app:$PYTHONPATH"
          ` + func() string {
		var envExports strings.Builder
		for key, value := range req.EnvVariables {
			envExports.WriteString(fmt.Sprintf("          export %s=\"%s\"\n", key, value))
		}
		return envExports.String()
	}() + `
          
          echo "=== Environment Check ==="
          echo "DJANGO_SETTINGS_MODULE: $DJANGO_SETTINGS_MODULE"
          echo "PYTHONPATH: $PYTHONPATH"
          echo "Working Directory: $(pwd)"
          
          echo "=== Testing WSGI Import ==="
          python3 -c "
          import sys
          from {{ django_wsgi_module }} import application
          print('WSGI application loaded successfully:', application)
          "
          
          echo "=== Starting Gunicorn ==="
          exec /home/azureuser/app/venv/bin/gunicorn {{ django_wsgi_module }}:application -c /home/azureuser/app/gunicorn.conf.py
        dest: /home/azureuser/app/start_gunicorn.sh
        owner: azureuser
        group: azureuser
        mode: '0755'

    - name: Test Gunicorn startup manually
      shell: |
        cd {{ django_project_path }}
        timeout 10s /home/azureuser/app/start_gunicorn.sh || echo "Gunicorn test completed"
      become_user: azureuser
      register: gunicorn_test
      ignore_errors: yes

    - name: Show Gunicorn test results
      debug:
        var: gunicorn_test

    - name: Create supervisor configuration for Django with Gunicorn
      copy:
        content: |
          [program:django-gunicorn]
          command=/home/azureuser/app/start_gunicorn.sh
          directory={{ django_project_path }}
          user=azureuser
          autostart=true
          autorestart=true
          redirect_stderr=false
          stdout_logfile=/home/azureuser/logs/django-gunicorn-stdout.log
          stdout_logfile_maxbytes=50MB
          stdout_logfile_backups=5
          stderr_logfile=/home/azureuser/logs/django-gunicorn-stderr.log
          stderr_logfile_maxbytes=50MB
          stderr_logfile_backups=5
          killasgroup=true
          stopasgroup=true
          stopsignal=TERM
          stopwaitsecs=10
          startretries=3
          startsecs=5
        dest: /etc/supervisor/conf.d/django-gunicorn.conf
        backup: yes
      notify: restart supervisor

    - name: Create nginx configuration
      copy:
        content: |
          server {
              listen 80;
              server_name _;
              client_max_body_size 100M;
              
              # Security headers
              add_header X-Frame-Options "SAMEORIGIN" always;
              add_header X-XSS-Protection "1; mode=block" always;
              add_header X-Content-Type-Options "nosniff" always;
              add_header Referrer-Policy "no-referrer-when-downgrade" always;
              
              # Increase timeout values
              proxy_connect_timeout 300s;
              proxy_send_timeout 300s;
              proxy_read_timeout 300s;
              proxy_buffering off;
              
              location / {
                  proxy_pass http://127.0.0.1:8000;
                  proxy_set_header Host $host;
                  proxy_set_header X-Real-IP $remote_addr;
                  proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
                  proxy_set_header X-Forwarded-Proto $scheme;
                  proxy_redirect off;
              }
              
              location /static/ {
                  alias {{ django_project_path }}/staticfiles/;
                  expires 30d;
                  add_header Cache-Control "public, no-transform";
              }
              
              location /media/ {
                  alias {{ django_project_path }}/media/;
                  expires 30d;
                  add_header Cache-Control "public, no-transform";
              }
              
              # Health check endpoint
              location /health/ {
                  access_log off;
                  return 200 "healthy\n";
                  add_header Content-Type text/plain;
              }
          }
        dest: /etc/nginx/sites-available/django
      notify: restart nginx

    - name: Enable nginx site
      file:
        src: /etc/nginx/sites-available/django
        dest: /etc/nginx/sites-enabled/django
        state: link
      notify: restart nginx

    - name: Remove default nginx site
      file:
        path: /etc/nginx/sites-enabled/default
        state: absent
      notify: restart nginx

    - name: Test nginx configuration
      command: nginx -t
      register: nginx_test
      ignore_errors: yes

    - name: Show nginx test result
      debug:
        var: nginx_test

    - name: Ensure supervisor is running
      systemd:
        name: supervisor
        state: started
        enabled: yes

    - name: Reload supervisor configuration
      shell: supervisorctl reread && supervisorctl update
      ignore_errors: yes

    - name: Wait for supervisor to process config
      pause:
        seconds: 5

    - name: Stop any existing django-gunicorn process
      supervisorctl:
        name: django-gunicorn
        state: stopped
      ignore_errors: yes

    - name: Wait before starting
      pause:
        seconds: 3

    - name: Start Django application with Gunicorn
      supervisorctl:
        name: django-gunicorn
        state: started
      register: gunicorn_start
      ignore_errors: yes

    - name: Wait for Django application to start
      pause:
        seconds: 10

    - name: Check Django application status with detailed logging
      shell: |
        echo "=== Supervisor Status ==="
        supervisorctl status django-gunicorn
        
        echo "=== Supervisor Logs ==="
        supervisorctl tail django-gunicorn stderr | head -20
        
        echo "=== Process Status ==="
        ps aux | grep -E "(gunicorn|python)" | grep -v grep
        
        echo "=== Port 8000 Status ==="
        ss -tlnp | grep :8000 || echo "Port 8000 not found"
        
        echo "=== Application Logs ==="
        tail -20 /home/azureuser/logs/django-gunicorn-stdout.log 2>/dev/null || echo "No stdout logs"
        echo "--- stderr ---"
        tail -20 /home/azureuser/logs/django-gunicorn-stderr.log 2>/dev/null || echo "No stderr logs"
        
        echo "=== Gunicorn Logs ==="
        tail -20 /home/azureuser/logs/gunicorn-error.log 2>/dev/null || echo "No gunicorn error logs"
        
        echo "=== Test Django Response ==="
        curl -I http://localhost:8000 || echo "Django not responding"
      register: django_status
      ignore_errors: yes

    - name: Show Django application status
      debug:
        var: django_status.stdout_lines

    - name: Ensure nginx is running
      systemd:
        name: nginx
        state: started
        enabled: yes

    - name: Test full application stack
      shell: |
        echo "=== Testing Nginx ==="
        curl -I http://localhost || echo "Nginx not responding"
        
        echo "=== Testing Django through Nginx ==="
        curl -s -o /dev/null -w "HTTP Status: %{http_code}\n" http://localhost || echo "Full stack test failed"
        
        echo "=== Recent Nginx Error Logs ==="
        tail -10 /var/log/nginx/error.log 2>/dev/null || echo "No nginx error logs"
      register: final_test
      ignore_errors: yes

    - name: Show final test results
      debug:
        var: final_test.stdout_lines

    - name: Display deployment summary
      debug:
        msg: |
          Deployment Summary:
          - Django Project: {{ django_project_name }}
          - Settings Module: {{ django_settings_module }}
          - WSGI Module: {{ django_wsgi_module }}
          - Project Path: {{ django_project_path }}
          - Server: Gunicorn with 3 workers
          - Web Server: Nginx
          - Application URL: http://{{ ansible_host }}
          - Logs: /home/azureuser/logs/

  handlers:
    - name: restart supervisor
      systemd:
        name: supervisor
        state: restarted

    - name: restart nginx
      systemd:
        name: nginx
        state: restarted
`)

	return playbookBuilder.String()
}

func (ds *DeploymentService) runAnsiblePlaybook(ansibleDir string) error {
	cmd := exec.Command("ansible-playbook", "-i", "inventory.ini", "playbook.yml", "-v")
	cmd.Dir = ansibleDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func extractRepoName(repoURL string) (string, error) {
	parsedURL, err := url.Parse(repoURL)
	if err != nil {
		return "", err
	}

	// Extract path and remove .git suffix if present
	path := strings.TrimPrefix(parsedURL.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	
	// Get the repo name (last part of the path)
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid repository URL format")
	}
	
	return parts[len(parts)-1], nil
}