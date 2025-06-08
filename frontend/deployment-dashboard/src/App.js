import React, { useState, useEffect, useRef } from 'react';
import { Play, Square, GitBranch, Server, Clock, CheckCircle, XCircle, AlertCircle, Plus, Trash2, Eye, EyeOff } from 'lucide-react';

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
  const [showToken, setShowToken] = useState(false);
  
  const [deploymentId, setDeploymentId] = useState(null);
  const [deploymentStatus, setDeploymentStatus] = useState('idle');
  const [logs, setLogs] = useState([]);
  const [isConnected, setIsConnected] = useState(false);
  const [error, setError] = useState(null);
  const [publicIP, setPublicIP] = useState(null);
  
  const eventSourceRef = useRef(null);
  const logsEndRef = useRef(null);

  useEffect(() => {
    logsEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [logs]);

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
        
        if (logMessage.level === 'system') {
          if (logMessage.message === 'DEPLOYMENT_COMPLETE') {
            setDeploymentStatus('completed');
            setIsConnected(false);
            eventSource.close();
            return;
          } else if (logMessage.message === 'heartbeat') {
            return;
          }
        }

        if (logMessage.level === 'success' && logMessage.message.includes('Public IP:')) {
          const ipMatch = logMessage.message.match(/Public IP: ([\d.]+)/);
          if (ipMatch) {
            setPublicIP(ipMatch[1]);
          }
        }

        setLogs(prev => [...prev, {
          ...logMessage,
          id: Date.now() + Math.random()
        }]);

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
        return <div className="animate-spin rounded-full h-5 w-5 border-2 border-blue-500 border-t-transparent"></div>;
      case 'completed':
        return <CheckCircle className="h-5 w-5 text-emerald-500" />;
      case 'failed':
        return <XCircle className="h-5 w-5 text-red-500" />;
      default:
        return <AlertCircle className="h-5 w-5 text-slate-400" />;
    }
  };

  const getStatusColor = (level) => {
    switch (level) {
      case 'error':
        return 'text-red-400 bg-red-900/20 border-red-500/30';
      case 'success':
        return 'text-emerald-400 bg-emerald-900/20 border-emerald-500/30';
      case 'warning':
        return 'text-amber-400 bg-amber-900/20 border-amber-500/30';
      case 'info':
        return 'text-blue-400 bg-blue-900/20 border-blue-500/30';
      case 'system':
        return 'text-slate-400 bg-slate-900/20 border-slate-500/30';
      default:
        return 'text-slate-300 bg-slate-900/20 border-slate-500/30';
    }
  };

  const formatTimestamp = (timestamp) => {
    return new Date(timestamp).toLocaleTimeString();
  };

  const getStatusBadgeColor = () => {
    switch (deploymentStatus) {
      case 'running':
        return 'bg-blue-100 text-blue-800 border-blue-200';
      case 'completed':
        return 'bg-emerald-100 text-emerald-800 border-emerald-200';
      case 'failed':
        return 'bg-red-100 text-red-800 border-red-200';
      case 'starting':
        return 'bg-amber-100 text-amber-800 border-amber-200';
      default:
        return 'bg-slate-100 text-slate-800 border-slate-200';
    }
  };

  return (
    <div className="min-h-screen bg-gradient-to-br from-slate-50 via-blue-50 to-indigo-100">
      {/* Header */}
      <div className="bg-white/80 backdrop-blur-sm border-b border-slate-200/50 sticky top-0 z-10">
        <div className="max-w-7xl mx-auto px-6 py-4">
          <div className="flex items-center gap-4">
            <div className="flex items-center gap-3">
              <div className="p-2 bg-gradient-to-br from-blue-500 to-indigo-600 rounded-xl">
                <Server className="h-6 w-6 text-white" />
              </div>
              <div>
                <h1 className="text-2xl font-bold bg-gradient-to-r from-slate-800 to-slate-600 bg-clip-text text-transparent">
                  Deployment Dashboard
                </h1>
                <p className="text-slate-600 text-sm">Deploy your applications with real-time monitoring</p>
              </div>
            </div>
            
            <div className="ml-auto flex items-center gap-4">
              <div className={`flex items-center gap-2 px-3 py-2 rounded-full border ${getStatusBadgeColor()}`}>
                {getStatusIcon()}
                <span className="font-medium text-sm capitalize">{deploymentStatus}</span>
              </div>
              
              {isConnected && (
                <div className="flex items-center gap-2 px-3 py-2 bg-emerald-100 text-emerald-800 rounded-full border border-emerald-200">
                  <div className="h-2 w-2 bg-emerald-500 rounded-full animate-pulse"></div>
                  <span className="text-sm font-medium">Live</span>
                </div>
              )}
            </div>
          </div>
        </div>
      </div>

      <div className="max-w-7xl mx-auto p-6">
        <div className="grid grid-cols-1 xl:grid-cols-3 gap-6 h-[calc(100vh-140px)]">
          {/* Configuration Panel */}
          <div className="xl:col-span-1 space-y-6">
            <div className="bg-white/70 backdrop-blur-sm rounded-2xl shadow-xl border border-white/20 p-6 h-full overflow-y-auto">
              <h2 className="text-xl font-bold flex items-center gap-3 mb-6 text-slate-800">
                <div className="p-2 bg-gradient-to-br from-indigo-500 to-purple-600 rounded-lg">
                  <GitBranch className="h-5 w-5 text-white" />
                </div>
                Configuration
              </h2>
              
              <div className="space-y-6">
                {/* Basic Fields */}
                <div className="space-y-4">
                  <div>
                    <label className="block text-sm font-semibold text-slate-700 mb-2">
                      Username *
                    </label>
                    <input
                      type="text"
                      name="username"
                      value={deploymentData.username}
                      onChange={handleInputChange}
                      className="w-full px-4 py-3 bg-white/80 border border-slate-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-blue-500/50 focus:border-blue-500 transition-all duration-200"
                      placeholder="Enter your username"
                      disabled={deploymentStatus === 'running'}
                    />
                  </div>

                  <div>
                    <label className="block text-sm font-semibold text-slate-700 mb-2">
                      Repository URL *
                    </label>
                    <input
                      type="url"
                      name="repo_url"
                      value={deploymentData.repo_url}
                      onChange={handleInputChange}
                      className="w-full px-4 py-3 bg-white/80 border border-slate-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-blue-500/50 focus:border-blue-500 transition-all duration-200"
                      placeholder="https://github.com/username/repository"
                      disabled={deploymentStatus === 'running'}
                    />
                  </div>

                  <div>
                    <label className="block text-sm font-semibold text-slate-700 mb-2">
                      GitHub Token *
                    </label>
                    <div className="relative">
                      <input
                        type={showToken ? "text" : "password"}
                        name="github_token"
                        value={deploymentData.github_token}
                        onChange={handleInputChange}
                        className="w-full px-4 py-3 pr-12 bg-white/80 border border-slate-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-blue-500/50 focus:border-blue-500 transition-all duration-200"
                        placeholder="Enter your GitHub personal access token"
                        disabled={deploymentStatus === 'running'}
                      />
                      <button
                        type="button"
                        onClick={() => setShowToken(!showToken)}
                        className="absolute right-3 top-1/2 transform -translate-y-1/2 text-slate-400 hover:text-slate-600 transition-colors"
                      >
                        {showToken ? <EyeOff className="h-5 w-5" /> : <Eye className="h-5 w-5" />}
                      </button>
                    </div>
                  </div>
                </div>

                {/* Additional Commands */}
                <div>
                  <label className="block text-sm font-semibold text-slate-700 mb-3">
                    Additional Commands
                  </label>
                  <div className="space-y-3">
                    <div className="flex gap-2">
                      <input
                        type="text"
                        value={newCommand}
                        onChange={(e) => setNewCommand(e.target.value)}
                        className="flex-1 px-4 py-3 bg-white/80 border border-slate-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-blue-500/50 focus:border-blue-500 transition-all duration-200"
                        placeholder="pip install requirements.txt"
                        disabled={deploymentStatus === 'running'}
                        onKeyPress={(e) => e.key === 'Enter' && addCommand()}
                      />
                      <button
                        type="button"
                        onClick={addCommand}
                        disabled={deploymentStatus === 'running' || !newCommand.trim()}
                        className="px-4 py-3 bg-gradient-to-r from-slate-600 to-slate-700 text-white rounded-xl hover:from-slate-700 hover:to-slate-800 disabled:opacity-50 disabled:cursor-not-allowed transition-all duration-200 flex items-center gap-2"
                      >
                        <Plus className="h-4 w-4" />
                      </button>
                    </div>
                    {deploymentData.additional_commands.length > 0 && (
                      <div className="space-y-2">
                        {deploymentData.additional_commands.map((command, index) => (
                          <div key={index} className="flex items-center gap-3 bg-slate-50 px-4 py-3 rounded-xl border border-slate-200">
                            <span className="flex-1 font-mono text-sm text-slate-700">{command}</span>
                            <button
                              type="button"
                              onClick={() => removeCommand(index)}
                              disabled={deploymentStatus === 'running'}
                              className="text-red-500 hover:text-red-700 disabled:opacity-50 transition-colors"
                            >
                              <Trash2 className="h-4 w-4" />
                            </button>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                </div>

                {/* Environment Variables */}
                <div>
                  <label className="block text-sm font-semibold text-slate-700 mb-3">
                    Environment Variables
                  </label>
                  <div className="space-y-3">
                    <div className="flex gap-2">
                      <input
                        type="text"
                        value={newEnvKey}
                        onChange={(e) => setNewEnvKey(e.target.value)}
                        className="flex-1 px-4 py-3 bg-white/80 border border-slate-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-blue-500/50 focus:border-blue-500 transition-all duration-200"
                        placeholder="KEY"
                        disabled={deploymentStatus === 'running'}
                      />
                      <input
                        type="text"
                        value={newEnvValue}
                        onChange={(e) => setNewEnvValue(e.target.value)}
                        className="flex-1 px-4 py-3 bg-white/80 border border-slate-200 rounded-xl focus:outline-none focus:ring-2 focus:ring-blue-500/50 focus:border-blue-500 transition-all duration-200"
                        placeholder="value"
                        disabled={deploymentStatus === 'running'}
                        onKeyPress={(e) => e.key === 'Enter' && addEnvVariable()}
                      />
                      <button
                        type="button"
                        onClick={addEnvVariable}
                        disabled={deploymentStatus === 'running' || !newEnvKey.trim() || !newEnvValue.trim()}
                        className="px-4 py-3 bg-gradient-to-r from-slate-600 to-slate-700 text-white rounded-xl hover:from-slate-700 hover:to-slate-800 disabled:opacity-50 disabled:cursor-not-allowed transition-all duration-200 flex items-center gap-2"
                      >
                        <Plus className="h-4 w-4" />
                      </button>
                    </div>
                    {Object.keys(deploymentData.env_variables).length > 0 && (
                      <div className="space-y-2">
                        {Object.entries(deploymentData.env_variables).map(([key, value]) => (
                          <div key={key} className="flex items-center gap-3 bg-slate-50 px-4 py-3 rounded-xl border border-slate-200">
                            <span className="font-mono text-sm text-slate-700">
                              <strong>{key}</strong> = {value}
                            </span>
                            <button
                              type="button"
                              onClick={() => removeEnvVariable(key)}
                              disabled={deploymentStatus === 'running'}
                              className="text-red-500 hover:text-red-700 disabled:opacity-50 ml-auto transition-colors"
                            >
                              <Trash2 className="h-4 w-4" />
                            </button>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                </div>

                {/* Configuration Options */}
                <div className="space-y-4">
                  <div className="flex items-center gap-3 p-4 bg-slate-50 rounded-xl border border-slate-200">
                    <input
                      type="checkbox"
                      id="asgi"
                      name="asgi"
                      checked={deploymentData.asgi}
                      onChange={handleInputChange}
                      disabled={deploymentStatus === 'running'}
                      className="h-5 w-5 text-blue-600 focus:ring-blue-500 border-slate-300 rounded"
                    />
                    <label htmlFor="asgi" className="text-sm font-medium text-slate-700 flex-1">
                      ASGI Application
                      <span className="block text-xs text-slate-500 mt-1">Use for Django Channels, FastAPI, etc.</span>
                    </label>
                  </div>

                  <div className="flex items-center gap-3 p-4 bg-slate-50 rounded-xl border border-slate-200">
                    <input
                      type="checkbox"
                      id="auto_deploy"
                      name="auto_deploy"
                      checked={deploymentData.auto_deploy}
                      onChange={handleInputChange}
                      disabled={deploymentStatus === 'running'}
                      className="h-5 w-5 text-blue-600 focus:ring-blue-500 border-slate-300 rounded"
                    />
                    <label htmlFor="auto_deploy" className="text-sm font-medium text-slate-700 flex-1">
                      Auto Deploy
                      <span className="block text-xs text-slate-500 mt-1">Automatically deploy after setup</span>
                    </label>
                  </div>
                </div>

                {error && (
                  <div className="bg-red-50 border border-red-200 rounded-xl p-4">
                    <div className="flex items-center gap-3">
                      <XCircle className="h-5 w-5 text-red-600 flex-shrink-0" />
                      <span className="text-red-700 text-sm">{error}</span>
                    </div>
                  </div>
                )}

                {/* Action Buttons */}
                <div className="flex gap-3 pt-4">
                  <button
                    onClick={startDeployment}
                    disabled={deploymentStatus === 'running' || deploymentStatus === 'starting'}
                    className="flex items-center gap-2 px-6 py-3 bg-gradient-to-r from-blue-600 to-indigo-600 text-white rounded-xl hover:from-blue-700 hover:to-indigo-700 disabled:opacity-50 disabled:cursor-not-allowed transition-all duration-200 shadow-lg hover:shadow-xl"
                  >
                    <Play className="h-5 w-5" />
                    {deploymentStatus === 'starting' ? 'Starting...' : 'Start Deployment'}
                  </button>

                  {deploymentStatus === 'running' && (
                    <button
                      onClick={stopDeployment}
                      className="flex items-center gap-2 px-6 py-3 bg-gradient-to-r from-red-600 to-red-700 text-white rounded-xl hover:from-red-700 hover:to-red-800 transition-all duration-200 shadow-lg hover:shadow-xl"
                    >
                      <Square className="h-5 w-5" />
                      Stop
                    </button>
                  )}
                </div>

                {/* Status Information */}
                {(deploymentId || publicIP) && (
                  <div className="bg-gradient-to-r from-blue-50 to-indigo-50 rounded-xl p-4 border border-blue-200 space-y-3">
                    <h3 className="font-semibold text-slate-800 flex items-center gap-2">
                      <Server className="h-4 w-4" />
                      Deployment Info
                    </h3>
                    
                    {deploymentId && (
                      <div className="text-sm text-slate-600">
                        <strong>ID:</strong> <span className="font-mono">{deploymentId}</span>
                      </div>
                    )}

                    {publicIP && (
                      <div className="text-sm text-emerald-700 bg-emerald-100 p-3 rounded-lg border border-emerald-200">
                        <strong>Public IP:</strong> <span className="font-mono">{publicIP}</span>
                      </div>
                    )}
                  </div>
                )}
              </div>
            </div>
          </div>

          {/* Logs Panel */}
          <div className="xl:col-span-2">
            <div className="bg-slate-900 rounded-2xl shadow-2xl border border-slate-700 h-full overflow-hidden">
              <div className="bg-gradient-to-r from-slate-800 to-slate-900 px-6 py-4 border-b border-slate-700">
                <h2 className="text-xl font-bold flex items-center gap-3 text-white">
                  <div className="p-2 bg-gradient-to-br from-emerald-500 to-green-600 rounded-lg">
                    <Clock className="h-5 w-5 text-white" />
                  </div>
                  Live Deployment Logs
                  {logs.length > 0 && (
                    <span className="ml-auto text-sm text-slate-400 bg-slate-800 px-3 py-1 rounded-full">
                      {logs.length} logs
                    </span>
                  )}
                </h2>
              </div>

              <div className="h-[calc(100%-80px)] overflow-y-auto p-4 font-mono text-sm">
                {logs.length === 0 ? (
                  <div className="text-slate-500 text-center py-12 space-y-4">
                    <div className="flex justify-center">
                      <div className="p-4 bg-slate-800 rounded-full">
                        <Clock className="h-8 w-8 text-slate-400" />
                      </div>
                    </div>
                    <div>
                      <p className="text-lg font-medium">No logs yet</p>
                      <p className="text-sm">Start a deployment to see live logs here</p>
                    </div>
                  </div>
                ) : (
                  <div className="space-y-2">
                    {logs.map((log) => (
                      <div key={log.id} className="flex gap-3 items-start group hover:bg-slate-800/50 p-2 rounded-lg transition-colors">
                        <span className="text-slate-500 text-xs shrink-0 mt-0.5 font-medium">
                          {formatTimestamp(log.timestamp)}
                        </span>
                        <span className={`text-xs px-2 py-1 rounded-md uppercase font-bold border ${getStatusColor(log.level)} shrink-0`}>
                          {log.level}
                        </span>
                        <span className="text-blue-400 text-xs bg-blue-900/20 px-2 py-1 rounded-md border border-blue-500/30 shrink-0">
                          {log.step}
                        </span>
                        <span className="text-slate-300 flex-1 leading-relaxed">{log.message}</span>
                      </div>
                    ))}
                    <div ref={logsEndRef} />
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};

export default DeploymentDashboard;