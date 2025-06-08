import React, { useState, useEffect, useRef } from 'react';
import { Play, Square, GitBranch, Server, Clock, CheckCircle, XCircle, AlertCircle } from 'lucide-react';

const DeploymentDashboard = () => {
  const [deploymentData, setDeploymentData] = useState({
    username: '',
    repo_url: '',
    github_token: '',
    additional_commands: [],
    env_variables: {},
    asgi: false,
    auto_deploy: true
  });
  
  const [newCommand, setNewCommand] = useState('');
  const [newEnvKey, setNewEnvKey] = useState('');
  const [newEnvValue, setNewEnvValue] = useState('');
  
  const [deploymentId, setDeploymentId] = useState(null);
  const [deploymentStatus, setDeploymentStatus] = useState('idle'); // idle, starting, running, completed, failed
  const [logs, setLogs] = useState([]);
  const [isConnected, setIsConnected] = useState(false);
  const [error, setError] = useState(null);
  const [publicIP, setPublicIP] = useState(null);
  
  const eventSourceRef = useRef(null);
  const logsEndRef = useRef(null);

  // Auto-scroll to bottom when new logs arrive
  useEffect(() => {
    logsEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [logs]);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (eventSourceRef.current) {
        eventSourceRef.current.close();
      }
    };
  }, []);

  const handleInputChange = (e) => {
    const { name, value, type, checked } = e.target;
    setDeploymentData(prev => ({
      ...prev,
      [name]: type === 'checkbox' ? checked : value
    }));
  };

  const addCommand = () => {
    if (newCommand.trim()) {
      setDeploymentData(prev => ({
        ...prev,
        additional_commands: [...prev.additional_commands, newCommand.trim()]
      }));
      setNewCommand('');
    }
  };

  const removeCommand = (index) => {
    setDeploymentData(prev => ({
      ...prev,
      additional_commands: prev.additional_commands.filter((_, i) => i !== index)
    }));
  };

  const addEnvVariable = () => {
    if (newEnvKey.trim() && newEnvValue.trim()) {
      setDeploymentData(prev => ({
        ...prev,
        env_variables: {
          ...prev.env_variables,
          [newEnvKey.trim()]: newEnvValue.trim()
        }
      }));
      setNewEnvKey('');
      setNewEnvValue('');
    }
  };

  const removeEnvVariable = (key) => {
    setDeploymentData(prev => {
      const newEnvVars = { ...prev.env_variables };
      delete newEnvVars[key];
      return {
        ...prev,
        env_variables: newEnvVars
      };
    });
  };

  const startDeployment = async () => {
    if (!deploymentData.username || !deploymentData.repo_url || !deploymentData.github_token) {
      setError('Please fill in all required fields (username, repo URL, and GitHub token)');
      return;
    }

    try {
      setError(null);
      setDeploymentStatus('starting');
      setLogs([]);
      setPublicIP(null);

      const response = await fetch('http://localhost:8080/deploy', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(deploymentData)
      });

      const result = await response.json();

      if (result.success) {
        setDeploymentId(result.deployment_id);
        setDeploymentStatus('running');
        connectToLogStream(result.deployment_id);
      } else {
        setError(result.error || 'Failed to start deployment');
        setDeploymentStatus('failed');
      }
    } catch (err) {
      setError(`Network error: ${err.message}`);
      setDeploymentStatus('failed');
    }
  };

  const connectToLogStream = (deploymentId) => {
    // Close existing connection if any
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
    }

    const eventSource = new EventSource(`http://localhost:8080/deploy/${deploymentId}/logs`);
    eventSourceRef.current = eventSource;

    eventSource.onopen = () => {
      setIsConnected(true);
      console.log('SSE connection opened');
    };

    eventSource.onmessage = (event) => {
      try {
        const logMessage = JSON.parse(event.data);
        
        // Handle system messages
        if (logMessage.level === 'system') {
          if (logMessage.message === 'DEPLOYMENT_COMPLETE') {
            setDeploymentStatus('completed');
            setIsConnected(false);
            eventSource.close();
            return;
          } else if (logMessage.message === 'heartbeat') {
            // Don't add heartbeat to logs
            return;
          }
        }

        // Handle success messages with public IP
        if (logMessage.level === 'success' && logMessage.message.includes('Public IP:')) {
          const ipMatch = logMessage.message.match(/Public IP: ([\d.]+)/);
          if (ipMatch) {
            setPublicIP(ipMatch[1]);
          }
        }

        // Add log to the list
        setLogs(prev => [...prev, {
          ...logMessage,
          id: Date.now() + Math.random()
        }]);

        // Update status based on log level
        if (logMessage.level === 'error') {
          setDeploymentStatus('failed');
        }

      } catch (err) {
        console.error('Error parsing SSE data:', err);
      }
    };

    eventSource.onerror = (err) => {
      console.error('SSE error:', err);
      setIsConnected(false);
      setError('Connection to log stream lost');
      eventSource.close();
    };
  };

  const stopDeployment = () => {
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
    }
    setIsConnected(false);
    setDeploymentStatus('idle');
    setDeploymentId(null);
    setLogs([]);
    setPublicIP(null);
  };

  const getStatusIcon = () => {
    switch (deploymentStatus) {
      case 'running':
        return <div className="animate-spin rounded-full h-4 w-4 border-2 border-blue-600 border-t-transparent"></div>;
      case 'completed':
        return <CheckCircle className="h-4 w-4 text-green-600" />;
      case 'failed':
        return <XCircle className="h-4 w-4 text-red-600" />;
      default:
        return <AlertCircle className="h-4 w-4 text-gray-400" />;
    }
  };

  const getStatusColor = (level) => {
    switch (level) {
      case 'error':
        return 'text-red-600 bg-red-50';
      case 'success':
        return 'text-green-600 bg-green-50';
      case 'warning':
        return 'text-yellow-600 bg-yellow-50';
      case 'info':
        return 'text-blue-600 bg-blue-50';
      case 'system':
        return 'text-gray-600 bg-gray-50';
      default:
        return 'text-gray-800 bg-white';
    }
  };

  const formatTimestamp = (timestamp) => {
    return new Date(timestamp).toLocaleTimeString();
  };

  return (
    <div className="min-h-screen bg-gray-100 p-6">
      <div className="max-w-6xl mx-auto">
        <div className="bg-white rounded-lg shadow-lg overflow-hidden">
          {/* Header */}
          <div className="bg-gradient-to-r from-blue-600 to-purple-600 text-white p-6">
            <div className="flex items-center gap-3">
              <Server className="h-8 w-8" />
              <div>
                <h1 className="text-2xl font-bold">Deployment Dashboard</h1>
                <p className="text-blue-100">Deploy your applications with real-time monitoring</p>
              </div>
            </div>
          </div>

          <div className="p-6 grid grid-cols-1 lg:grid-cols-2 gap-6">
            {/* Deployment Form */}
            <div className="space-y-4">
              <h2 className="text-xl font-semibold flex items-center gap-2">
                <GitBranch className="h-5 w-5" />
                Deployment Configuration
              </h2>
              
              <div className="space-y-4">
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-2">
                    Username *
                  </label>
                  <input
                    type="text"
                    name="username"
                    value={deploymentData.username}
                    onChange={handleInputChange}
                    className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                    placeholder="Enter your username"
                    disabled={deploymentStatus === 'running'}
                  />
                </div>

                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-2">
                    Repository URL *
                  </label>
                  <input
                    type="url"
                    name="repo_url"
                    value={deploymentData.repo_url}
                    onChange={handleInputChange}
                    className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                    placeholder="https://github.com/username/repository"
                    disabled={deploymentStatus === 'running'}
                  />
                </div>

                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-2">
                    GitHub Token *
                  </label>
                  <input
                    type="password"
                    name="github_token"
                    value={deploymentData.github_token}
                    onChange={handleInputChange}
                    className="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                    placeholder="Enter your GitHub personal access token"
                    disabled={deploymentStatus === 'running'}
                  />
                </div>

                {/* Additional Commands */}
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-2">
                    Additional Commands
                  </label>
                  <div className="space-y-2">
                    <div className="flex gap-2">
                      <input
                        type="text"
                        value={newCommand}
                        onChange={(e) => setNewCommand(e.target.value)}
                        className="flex-1 px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                        placeholder="pip install requirements.txt"
                        disabled={deploymentStatus === 'running'}
                        onKeyPress={(e) => e.key === 'Enter' && addCommand()}
                      />
                      <button
                        type="button"
                        onClick={addCommand}
                        disabled={deploymentStatus === 'running' || !newCommand.trim()}
                        className="px-3 py-2 bg-gray-600 text-white rounded-md hover:bg-gray-700 disabled:opacity-50 disabled:cursor-not-allowed"
                      >
                        Add
                      </button>
                    </div>
                    {deploymentData.additional_commands.length > 0 && (
                      <div className="space-y-1">
                        {deploymentData.additional_commands.map((command, index) => (
                          <div key={index} className="flex items-center gap-2 bg-gray-50 px-3 py-2 rounded">
                            <span className="flex-1 font-mono text-sm">{command}</span>
                            <button
                              type="button"
                              onClick={() => removeCommand(index)}
                              disabled={deploymentStatus === 'running'}
                              className="text-red-600 hover:text-red-800 disabled:opacity-50"
                            >
                              Remove
                            </button>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                </div>

                {/* Environment Variables */}
                <div>
                  <label className="block text-sm font-medium text-gray-700 mb-2">
                    Environment Variables
                  </label>
                  <div className="space-y-2">
                    <div className="flex gap-2">
                      <input
                        type="text"
                        value={newEnvKey}
                        onChange={(e) => setNewEnvKey(e.target.value)}
                        className="flex-1 px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                        placeholder="KEY"
                        disabled={deploymentStatus === 'running'}
                      />
                      <input
                        type="text"
                        value={newEnvValue}
                        onChange={(e) => setNewEnvValue(e.target.value)}
                        className="flex-1 px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                        placeholder="value"
                        disabled={deploymentStatus === 'running'}
                        onKeyPress={(e) => e.key === 'Enter' && addEnvVariable()}
                      />
                      <button
                        type="button"
                        onClick={addEnvVariable}
                        disabled={deploymentStatus === 'running' || !newEnvKey.trim() || !newEnvValue.trim()}
                        className="px-3 py-2 bg-gray-600 text-white rounded-md hover:bg-gray-700 disabled:opacity-50 disabled:cursor-not-allowed"
                      >
                        Add
                      </button>
                    </div>
                    {Object.keys(deploymentData.env_variables).length > 0 && (
                      <div className="space-y-1">
                        {Object.entries(deploymentData.env_variables).map(([key, value]) => (
                          <div key={key} className="flex items-center gap-2 bg-gray-50 px-3 py-2 rounded">
                            <span className="font-mono text-sm">
                              <strong>{key}</strong> = {value}
                            </span>
                            <button
                              type="button"
                              onClick={() => removeEnvVariable(key)}
                              disabled={deploymentStatus === 'running'}
                              className="text-red-600 hover:text-red-800 disabled:opacity-50 ml-auto"
                            >
                              Remove
                            </button>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                </div>

                {/* Configuration Options */}
                <div className="space-y-3">
                  <div className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      id="asgi"
                      name="asgi"
                      checked={deploymentData.asgi}
                      onChange={handleInputChange}
                      disabled={deploymentStatus === 'running'}
                      className="h-4 w-4 text-blue-600 focus:ring-blue-500 border-gray-300 rounded"
                    />
                    <label htmlFor="asgi" className="text-sm font-medium text-gray-700">
                      ASGI Application (Use for Django Channels, FastAPI, etc.)
                    </label>
                  </div>

                  <div className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      id="auto_deploy"
                      name="auto_deploy"
                      checked={deploymentData.auto_deploy}
                      onChange={handleInputChange}
                      disabled={deploymentStatus === 'running'}
                      className="h-4 w-4 text-blue-600 focus:ring-blue-500 border-gray-300 rounded"
                    />
                    <label htmlFor="auto_deploy" className="text-sm font-medium text-gray-700">
                      Auto Deploy (Automatically deploy after setup)
                    </label>
                  </div>
                </div>

                {error && (
                  <div className="bg-red-50 border border-red-200 rounded-md p-3">
                    <div className="flex items-center gap-2">
                      <XCircle className="h-4 w-4 text-red-600" />
                      <span className="text-red-700 text-sm">{error}</span>
                    </div>
                  </div>
                )}

                <div className="flex gap-3">
                  <button
                    onClick={startDeployment}
                    disabled={deploymentStatus === 'running' || deploymentStatus === 'starting'}
                    className="flex items-center gap-2 px-4 py-2 bg-blue-600 text-white rounded-md hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
                  >
                    <Play className="h-4 w-4" />
                    {deploymentStatus === 'starting' ? 'Starting...' : 'Start Deployment'}
                  </button>

                  {deploymentStatus === 'running' && (
                    <button
                      onClick={stopDeployment}
                      className="flex items-center gap-2 px-4 py-2 bg-red-600 text-white rounded-md hover:bg-red-700 transition-colors"
                    >
                      <Square className="h-4 w-4" />
                      Stop
                    </button>
                  )}
                </div>
              </div>

              {/* Status Panel */}
              <div className="bg-gray-50 rounded-lg p-4 space-y-3">
                <h3 className="font-semibold text-gray-800">Deployment Status</h3>
                
                <div className="flex items-center gap-2">
                  {getStatusIcon()}
                  <span className="font-medium capitalize">{deploymentStatus}</span>
                  {isConnected && (
                    <span className="text-sm text-green-600 bg-green-100 px-2 py-1 rounded-full">
                      Connected
                    </span>
                  )}
                </div>

                {deploymentId && (
                  <div className="text-sm text-gray-600">
                    <strong>ID:</strong> {deploymentId}
                  </div>
                )}

                {publicIP && (
                  <div className="text-sm text-green-700 bg-green-100 p-2 rounded">
                    <strong>Public IP:</strong> {publicIP}
                  </div>
                )}
              </div>
            </div>

            {/* Logs Panel */}
            <div className="space-y-4">
              <h2 className="text-xl font-semibold flex items-center gap-2">
                <Clock className="h-5 w-5" />
                Live Deployment Logs
              </h2>

              <div className="bg-black text-green-400 rounded-lg h-96 overflow-y-auto p-4 font-mono text-sm">
                {logs.length === 0 ? (
                  <div className="text-gray-500 text-center py-8">
                    No logs yet. Start a deployment to see live logs here.
                  </div>
                ) : (
                  <div className="space-y-1">
                    {logs.map((log) => (
                      <div key={log.id} className="flex gap-2">
                        <span className="text-gray-500 text-xs shrink-0">
                          {formatTimestamp(log.timestamp)}
                        </span>
                        <span className={`text-xs px-2 py-1 rounded uppercase ${getStatusColor(log.level)} text-black`}>
                          {log.level}
                        </span>
                        <span className="text-blue-400 text-xs">
                          [{log.step}]
                        </span>
                        <span className="flex-1">{log.message}</span>
                      </div>
                    ))}
                    <div ref={logsEndRef} />
                  </div>
                )}
              </div>

              {logs.length > 0 && (
                <div className="text-sm text-gray-600">
                  Total logs: {logs.length}
                </div>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};

export default DeploymentDashboard;