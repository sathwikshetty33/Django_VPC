package services

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (ds *DeploymentService) createAnsibleFiles(ansibleDir string, req *DeploymentRequest, publicIP string) error {
	inventoryContent := fmt.Sprintf(`[django_servers]
%s ansible_user=azureuser ansible_password=P@ssw0rd1234! ansible_connection=ssh ansible_ssh_common_args='-o StrictHostKeyChecking=no'
`, publicIP)

	inventoryPath := filepath.Join(ansibleDir, "inventory.ini")
	if err := os.WriteFile(inventoryPath, []byte(inventoryContent), 0644); err != nil {
		return fmt.Errorf("failed to write inventory file: %v", err)
	}

	playbookContent := ds.generatePlaybook(req, publicIP)
	playbookPath := filepath.Join(ansibleDir, "playbook.yml")
	if err := os.WriteFile(playbookPath, []byte(playbookContent), 0644); err != nil {
		return fmt.Errorf("failed to write playbook file: %v", err)
	}

	return nil
}

func (ds *DeploymentService) generatePlaybook(req *DeploymentRequest, publicIP string) string {
	var envVars strings.Builder
	for key, value := range req.EnvVariables {
		envVars.WriteString(fmt.Sprintf("      %s: \"%s\"\n", key, value))
	}

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

	serverType := "Gunicorn"
	
	if req.ASGI {
		serverType = "Gunicorn+Uvicorn"
	}

	var playbookBuilder strings.Builder
	
	playbookBuilder.WriteString(`---
- name: Deploy Django Application with ` + serverType + `
  hosts: django_servers
  become: yes
  vars:
    repo_url: "` + req.RepoURL + `"
    github_token: "` + req.GithubToken + `"
    public_ip: "` + publicIP + `"
    asgi: ` + fmt.Sprintf("%t", req.ASGI) + `
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

    - name: Remove existing virtual environment if it exists
      file:
        path: /home/azureuser/app/venv
        state: absent

    - name: Create fresh virtual environment
      command: python3 -m venv venv
      args:
        chdir: /home/azureuser/app
        creates: /home/azureuser/app/venv/bin/python3
      become_user: azureuser

    - name: Verify virtual environment creation
      stat:
        path: /home/azureuser/app/venv/bin/python3
      register: venv_check

    - name: Fail if virtual environment not created properly
      fail:
        msg: "Virtual environment was not created properly"
      when: not venv_check.stat.exists

    - name: Upgrade pip in virtual environment
      shell: |
        source /home/azureuser/app/venv/bin/activate
        python -m pip install --upgrade pip
      args:
        chdir: /home/azureuser/app
        executable: /bin/bash
      become_user: azureuser

    - name: Find requirements.txt file
      find:
        paths: /home/azureuser/app
        recurse: yes
        file_type: file
        patterns: "requirements.txt"
      register: requirements_files

    - name: Set requirements file path
      set_fact:
        actual_req_path: "{{ requirements_files.files[0].path if requirements_files.files | length > 0 else '/home/azureuser/app/requirements.txt' }}"

    - name: Install Python dependencies
      shell: |
        source /home/azureuser/app/venv/bin/activate
        python -m pip install -r "{{ actual_req_path }}" --no-cache-dir
      args:
        chdir: /home/azureuser/app
        executable: /bin/bash
      become_user: azureuser

    - name: Install server packages
      shell: |
        source /home/azureuser/app/venv/bin/activate
        python -m pip install gunicorn psycopg2-binary whitenoise django-cors-headers
      args:
        chdir: /home/azureuser/app
        executable: /bin/bash
      become_user: azureuser

    - name: Install ASGI packages if needed
      shell: |
        source /home/azureuser/app/venv/bin/activate
        python -m pip install "uvicorn[standard]"
      args:
        chdir: /home/azureuser/app
        executable: /bin/bash
      become_user: azureuser
      when: asgi

    - name: Find Django manage.py file
      find:
        paths: /home/azureuser/app
        recurse: yes
        patterns: "manage.py"
      register: manage_files

    - name: Set Django project path
      set_fact:
        django_project_path: "{{ manage_files.files[0].path | dirname if manage_files.files | length > 0 else '/home/azureuser/app' }}"

    - name: Extract Django project configuration
      shell: |
        cd "{{ django_project_path }}"
        source /home/azureuser/app/venv/bin/activate
        export PYTHONPATH="/home/azureuser/app:$PYTHONPATH"
        
        python3 -c "
        import os, sys, re
        
        # Get project name from manage.py
        with open('manage.py', 'r') as f:
            content = f.read()
        
        settings_match = re.search(r'[\"\\']([^\"\\'\s]+\.settings)[\"\\']', content)
        if settings_match:
            settings_module = settings_match.group(1)
            project_name = settings_module.split('.')[0]
        else:
            project_name = os.path.basename('{{ django_project_path }}')
            settings_module = project_name + '.settings'
        
        # Find WSGI files - look for project-specific ones first
        wsgi_files = []
        for root, dirs, files in os.walk('.'):
            # Skip virtual environment and other irrelevant directories
            if 'venv' in root or '__pycache__' in root or '.git' in root:
                continue
            for file in files:
                if file == 'wsgi.py':
                    rel_path = os.path.relpath(os.path.join(root, file), '.')
                    # Convert file path to module path
                    module_path = rel_path.replace('/', '.').replace('.py', '')
                    wsgi_files.append(module_path)
        
        # Find ASGI files - look for project-specific ones first  
        asgi_files = []
        for root, dirs, files in os.walk('.'):
            # Skip virtual environment and other irrelevant directories
            if 'venv' in root or '__pycache__' in root or '.git' in root:
                continue
            for file in files:
                if file == 'asgi.py':
                    rel_path = os.path.relpath(os.path.join(root, file), '.')
                    # Convert file path to module path
                    module_path = rel_path.replace('/', '.').replace('.py', '')
                    asgi_files.append(module_path)
        
        # Prefer project-specific modules over generic ones
        wsgi_module = None
        asgi_module = None
        
        # For WSGI, prefer modules that contain the project name
        for module in wsgi_files:
            if project_name in module:
                wsgi_module = module
                break
        if not wsgi_module and wsgi_files:
            wsgi_module = wsgi_files[0]
        if not wsgi_module:
            wsgi_module = project_name + '.wsgi'
        
        # For ASGI, prefer modules that contain the project name
        for module in asgi_files:
            if project_name in module:
                asgi_module = module
                break
        if not asgi_module and asgi_files:
            asgi_module = asgi_files[0]
        if not asgi_module:
            asgi_module = project_name + '.asgi'
        
        print(f'{project_name}|{settings_module}|{wsgi_module}|{asgi_module}')
        "
      register: django_config
      become_user: azureuser

    - name: Parse Django configuration
      set_fact:
        django_project_name: "{{ django_config.stdout.split('|')[0] if '|' in django_config.stdout else 'myproject' }}"
        django_settings_module: "{{ django_config.stdout.split('|')[1] if '|' in django_config.stdout else 'myproject.settings' }}"
        django_wsgi_module: "{{ django_config.stdout.split('|')[2] if django_config.stdout.split('|') | length > 2 else 'myproject.wsgi' }}"
        django_asgi_module: "{{ django_config.stdout.split('|')[3] if django_config.stdout.split('|') | length > 3 else 'myproject.asgi' }}"

    - name: Debug Django configuration
      debug:
        msg: |
          Project: {{ django_project_name }}
          Settings: {{ django_settings_module }}
          WSGI: {{ django_wsgi_module }}
          ASGI: {{ django_asgi_module }}

    - name: Update Django settings for ALLOWED_HOSTS and CORS
      shell: |
        cd "{{ django_project_path }}"
        source /home/azureuser/app/venv/bin/activate
        export PYTHONPATH="/home/azureuser/app:$PYTHONPATH"
        
        python3 -c "
        import os, sys, re
        
        settings_file = '{{ django_settings_module }}'.replace('.', '/') + '.py'
        if not os.path.exists(settings_file):
            for root, dirs, files in os.walk('.'):
                for file in files:
                    if file == 'settings.py':
                        settings_file = os.path.join(root, file)
                        break
        
        if os.path.exists(settings_file):
            with open(settings_file, 'r') as f:
                content = f.read()
            
            # Update ALLOWED_HOSTS
            allowed_hosts_match = re.search(r'ALLOWED_HOSTS\s*=\s*\[(.*?)\]', content, re.DOTALL)
            if allowed_hosts_match:
                hosts_content = allowed_hosts_match.group(1).strip()
                if hosts_content != \"'*'\" and \"'*'\" not in hosts_content:
                    if hosts_content:
                        hosts_content = hosts_content.rstrip().rstrip(',')
                        new_hosts = f'[{hosts_content}, \"{{ public_ip }}\"]'
                    else:
                        new_hosts = f'[\"{{ public_ip }}\"]'
                    content = re.sub(r'ALLOWED_HOSTS\s*=\s*\[.*?\]', f'ALLOWED_HOSTS = {new_hosts}', content, flags=re.DOTALL)
            else:
                content += f\"\\nALLOWED_HOSTS = ['{{ public_ip }}']\\n\"
            
            # Add corsheaders to INSTALLED_APPS if not present
            if 'corsheaders' not in content:
                installed_apps_match = re.search(r'INSTALLED_APPS\s*=\s*\[(.*?)\]', content, re.DOTALL)
                if installed_apps_match:
                    apps_content = installed_apps_match.group(1).strip()
                    apps_content = apps_content.rstrip().rstrip(',')
                    new_apps = f'[{apps_content},\\n    \"corsheaders\"]'
                    content = re.sub(r'INSTALLED_APPS\s*=\s*\[.*?\]', f'INSTALLED_APPS = {new_apps}', content, flags=re.DOTALL)
                else:
                    content += \"\\nINSTALLED_APPS.append('corsheaders')\\n\"
            
            # Add CORS middleware if not present
            if 'corsheaders.middleware.CorsMiddleware' not in content:
                middleware_match = re.search(r'MIDDLEWARE\s*=\s*\[(.*?)\]', content, re.DOTALL)
                if middleware_match:
                    middleware_content = middleware_match.group(1).strip()
                    middleware_content = middleware_content.rstrip().rstrip(',')
                    new_middleware = f'[\"corsheaders.middleware.CorsMiddleware\",\\n    {middleware_content}]'
                    content = re.sub(r'MIDDLEWARE\s*=\s*\[.*?\]', f'MIDDLEWARE = {new_middleware}', content, flags=re.DOTALL)
                else:
                    content += \"\\nMIDDLEWARE = ['corsheaders.middleware.CorsMiddleware'] + MIDDLEWARE\\n\"
            
            # Handle CORS settings
            cors_allow_all_match = re.search(r'CORS_ALLOW_ALL_ORIGINS\s*=\s*(True|False)', content)
            if cors_allow_all_match:
                if cors_allow_all_match.group(1) != 'True':
                    cors_allowed_match = re.search(r'CORS_ALLOWED_ORIGINS\s*=\s*\[(.*?)\]', content, re.DOTALL)
                    if cors_allowed_match:
                        origins_content = cors_allowed_match.group(1).strip()
                        origins_content = origins_content.rstrip().rstrip(',')
                        new_origins = f'[{origins_content},\\n    \"http://{{ public_ip }}\"]'
                        content = re.sub(r'CORS_ALLOWED_ORIGINS\s*=\s*\[.*?\]', f'CORS_ALLOWED_ORIGINS = {new_origins}', content, flags=re.DOTALL)
                    else:
                        content += f\"\\nCORS_ALLOWED_ORIGINS = [\\n    'http://{{ public_ip }}'\\n]\\n\"
            else:
                content += f\"\\nCORS_ALLOWED_ORIGINS = [\\n    'http://{{ public_ip }}'\\n]\\n\"
            
            with open(settings_file, 'w') as f:
                f.write(content)
        "
      args:
        executable: /bin/bash
      become_user: azureuser

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
        - { path: "/home/azureuser/logs/server-access.log", state: "touch", owner: "azureuser", group: "azureuser", mode: "0644" }
        - { path: "/home/azureuser/logs/server-error.log", state: "touch", owner: "azureuser", group: "azureuser", mode: "0644" }
        - { path: "/home/azureuser/logs/django-server-stdout.log", state: "touch", owner: "azureuser", group: "azureuser", mode: "0644" }
        - { path: "/home/azureuser/logs/django-server-stderr.log", state: "touch", owner: "azureuser", group: "azureuser", mode: "0644" }

    - name: Run Django migrations
      shell: |
        cd "{{ django_project_path }}"
        source /home/azureuser/app/venv/bin/activate
        export DJANGO_SETTINGS_MODULE="{{ django_settings_module }}"
        export PYTHONPATH="/home/azureuser/app:$PYTHONPATH"
        python manage.py migrate --noinput
      args:
        executable: /bin/bash
      become_user: azureuser
      environment: "{{ env_vars }}"

    - name: Collect static files
      shell: |
        cd "{{ django_project_path }}"
        source /home/azureuser/app/venv/bin/activate
        export DJANGO_SETTINGS_MODULE="{{ django_settings_module }}"
        export PYTHONPATH="/home/azureuser/app:$PYTHONPATH"
        python manage.py collectstatic --noinput
      args:
        executable: /bin/bash
      become_user: azureuser
      environment: "{{ env_vars }}"
      ignore_errors: yes`)

	if len(req.AdditionalCommands) > 0 {
		playbookBuilder.WriteString(additionalTasks.String())
	}

	if req.ASGI {
		playbookBuilder.WriteString(`

    - name: Verify ASGI module can be imported
      shell: |
        cd "{{ django_project_path }}"
        source /home/azureuser/app/venv/bin/activate
        export DJANGO_SETTINGS_MODULE="{{ django_settings_module }}"
        export PYTHONPATH="/home/azureuser/app:$PYTHONPATH"
        python -c "
        import django
        django.setup()
        import importlib
        import sys
        import os
        
        try:
            # Try to import the ASGI module
            asgi_module = importlib.import_module('{{ django_asgi_module }}')
            print('ASGI module imported successfully: {{ django_asgi_module }}')
            
            # Check if it has the application attribute
            if hasattr(asgi_module, 'application'):
                app = asgi_module.application
                print('ASGI application found')
            else:
                print('Warning: ASGI application attribute not found')
                # List available attributes for debugging
                attrs = [attr for attr in dir(asgi_module) if not attr.startswith('_')]
                print(f'Available attributes: {attrs}')
                
        except ImportError as e:
            print(f'Error importing ASGI module {{ django_asgi_module }}: {e}')
            
            # Try to find and suggest alternative ASGI modules
            print('Searching for available ASGI modules...')
            for root, dirs, files in os.walk('.'):
                if 'venv' in root or '__pycache__' in root:
                    continue
                for file in files:
                    if file == 'asgi.py':
                        asgi_path = os.path.join(root, file)
                        rel_path = os.path.relpath(asgi_path, '.')
                        module_path = rel_path.replace('/', '.').replace('.py', '')
                        print(f'Found ASGI file: {module_path}')
            raise
        "
      args:
        executable: /bin/bash
      become_user: azureuser
      environment: "{{ env_vars }}"

    - name: Create ASGI startup script
      copy:
        content: |
          #!/bin/bash
          set -e
          
          echo "Starting Django ASGI server..."
          cd "{{ django_project_path }}"
          
          # Activate virtual environment using absolute path
          source /home/azureuser/app/venv/bin/activate
          
          # Set environment variables
          export DJANGO_SETTINGS_MODULE="{{ django_settings_module }}"
          export PYTHONPATH="/home/azureuser/app:$PYTHONPATH"
          ` + func() string {
		var envExports strings.Builder
		for key, value := range req.EnvVariables {
			envExports.WriteString(fmt.Sprintf("          export %s=\"%s\"\n", key, value))
		}
		return envExports.String()
	}() + `
          
          # Use absolute path to gunicorn with corrected arguments
          exec /home/azureuser/app/venv/bin/gunicorn {{ django_asgi_module }}:application \
            --bind 0.0.0.0:8000 \
            --workers 3 \
            --worker-class uvicorn.workers.UvicornWorker \
            --worker-connections 1000 \
            --timeout 300 \
            --max-requests 1000 \
            --max-requests-jitter 50 \
            --preload \
            --access-logfile /home/azureuser/logs/server-access.log \
            --error-logfile /home/azureuser/logs/server-error.log \
            --log-level info \
            --user azureuser \
            --group azureuser
        dest: /home/azureuser/app/start_server.sh
        owner: azureuser
        group: azureuser
        mode: '0755'`)
	} else {
		playbookBuilder.WriteString(`

    - name: Create WSGI startup script
      copy:
        content: |
          #!/bin/bash
          set -e
          
          echo "Starting Django WSGI server..."
          cd "{{ django_project_path }}"
          
          # Activate virtual environment using absolute path
          source /home/azureuser/app/venv/bin/activate
          
          # Set environment variables
          export DJANGO_SETTINGS_MODULE="{{ django_settings_module }}"
          export PYTHONPATH="/home/azureuser/app:$PYTHONPATH"
          ` + func() string {
		var envExports strings.Builder
		for key, value := range req.EnvVariables {
			envExports.WriteString(fmt.Sprintf("          export %s=\"%s\"\n", key, value))
		}
		return envExports.String()
	}() + `
          
          # Use absolute path to gunicorn with corrected arguments
          exec /home/azureuser/app/venv/bin/gunicorn {{ django_wsgi_module }}:application \
            --bind 0.0.0.0:8000 \
            --workers 3 \
            --worker-class sync \
            --worker-connections 1000 \
            --timeout 300 \
            --max-requests 1000 \
            --max-requests-jitter 50 \
            --preload \
            --access-logfile /home/azureuser/logs/server-access.log \
            --error-logfile /home/azureuser/logs/server-error.log \
            --log-level info \
            --user azureuser \
            --group azureuser
        dest: /home/azureuser/app/start_server.sh
        owner: azureuser
        group: azureuser
        mode: '0755'`)
	}

	playbookBuilder.WriteString(`

    - name: Create supervisor configuration for Django
      copy:
        content: |
          [program:django-server]
          command=/home/azureuser/app/start_server.sh
          directory={{ django_project_path }}
          user=azureuser
          autostart=true
          autorestart=true
          redirect_stderr=false
          stdout_logfile=/home/azureuser/logs/django-server-stdout.log
          stdout_logfile_maxbytes=50MB
          stdout_logfile_backups=5
          stderr_logfile=/home/azureuser/logs/django-server-stderr.log
          stderr_logfile_maxbytes=50MB
          stderr_logfile_backups=5
          killasgroup=true
          stopasgroup=true
          stopsignal=TERM
          stopwaitsecs=10
          startretries=3
          startsecs=10
          environment=HOME="/home/azureuser",USER="azureuser",PATH="/home/azureuser/app/venv/bin:/usr/local/bin:/usr/bin:/bin"
        dest: /etc/supervisor/conf.d/django-server.conf
        backup: yes
      notify: restart supervisor

    - name: Create nginx configuration
      copy:
        content: |
          server {
              listen 80;
              server_name _;
              client_max_body_size 100M;
              
              add_header X-Frame-Options "SAMEORIGIN" always;
              add_header X-XSS-Protection "1; mode=block" always;
              add_header X-Content-Type-Options "nosniff" always;
              add_header Referrer-Policy "no-referrer-when-downgrade" always;
              
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

    - name: Stop any existing django-server process
      supervisorctl:
        name: django-server
        state: stopped
      ignore_errors: yes

    - name: Wait before starting
      pause:
        seconds: 3

    - name: Start Django application
      supervisorctl:
        name: django-server
        state: started
      register: server_start
      ignore_errors: yes

    - name: Wait for Django application to start
      pause:
        seconds: 10

    - name: Check final server status
      shell: supervisorctl status django-server
      register: final_status
      ignore_errors: yes

    - name: Display final status
      debug:
        msg: "Server status: {{ final_status.stdout }}"

    - name: Show recent logs if server failed to start
      shell: |
        echo "=== STDOUT ==="
        tail -n 20 /home/azureuser/logs/django-server-stdout.log 2>/dev/null || echo "No stdout log"
        echo "=== STDERR ==="
        tail -n 20 /home/azureuser/logs/django-server-stderr.log 2>/dev/null || echo "No stderr log"
      register: debug_logs
      when: final_status.stdout is defined and 'RUNNING' not in final_status.stdout

    - name: Display debug logs
      debug:
        msg: "{{ debug_logs.stdout_lines }}"
      when: final_status.stdout is defined and 'RUNNING' not in final_status.stdout

    - name: Ensure nginx is running
      systemd:
        name: nginx
        state: started
        enabled: yes

    - name: Display deployment summary
      debug:
        msg: |
          Deployment Summary:
          - Django Project: {{ django_project_name }}
          - Settings Module: {{ django_settings_module }}
          - Server Type: ` + serverType + `
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