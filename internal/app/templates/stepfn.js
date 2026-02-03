// Step Functions Local Executor for RDS Maintenance Machine Demo
// This module simulates AWS Step Functions execution locally by:
// 1. Parsing the ASL state machine definition
// 2. Executing states in sequence (Task -> API calls, Choice -> conditionals, Wait -> timeouts)
// 3. Emitting events for UI visualization

class StepFunctionExecutor {
    constructor() {
        this.definition = null;
        this.currentState = null;
        this.stateData = {};
        this.history = [];
        this.isRunning = false;
        this.isPaused = false;
        this.speedMultiplier = 1;
        this.manualMode = false;
        this.pendingStep = null;
        this.interventionCallback = null;
        this.listeners = {
            stateChange: [],
            historyUpdate: [],
            executionComplete: [],
            error: []
        };
    }

    // Event handling
    on(event, callback) {
        if (this.listeners[event]) {
            this.listeners[event].push(callback);
        }
    }

    emit(event, data) {
        if (this.listeners[event]) {
            this.listeners[event].forEach(cb => cb(data));
        }
    }

    // Load the state machine definition
    async loadDefinition() {
        try {
            const response = await fetch('/api/sf/definition');
            this.definition = await response.json();
            return this.definition;
        } catch (err) {
            this.emit('error', { message: 'Failed to load SF definition', error: err });
            throw err;
        }
    }

    // Start a new execution with given input
    async start(input = {}) {
        if (!this.definition) {
            await this.loadDefinition();
        }

        this.isRunning = true;
        this.isPaused = false;
        this.history = [];
        this.stateData = { ...input };
        this.currentState = this.definition.StartAt;

        this.addHistory('ExecutionStarted', `Starting at ${this.currentState}`, input);
        this.emit('stateChange', { state: this.currentState, data: this.stateData, status: 'entering' });

        if (!this.manualMode) {
            await this.runToCompletion();
        }
    }

    // Run until completion or pause
    async runToCompletion() {
        while (this.isRunning && !this.isPaused && this.currentState) {
            await this.executeCurrentState();
            
            // Small delay between states for visualization
            if (this.isRunning && !this.isPaused) {
                await this.sleep(100 / this.speedMultiplier);
            }
        }
    }

    // Execute a single step (for manual mode)
    async step() {
        if (!this.isRunning || !this.currentState) {
            return false;
        }
        await this.executeCurrentState();
        return this.isRunning;
    }

    // Execute the current state
    async executeCurrentState() {
        const stateName = this.currentState;
        const stateConfig = this.definition.States[stateName];

        if (!stateConfig) {
            this.emit('error', { message: `Unknown state: ${stateName}` });
            this.isRunning = false;
            return;
        }

        const startTime = Date.now();
        this.emit('stateChange', { state: stateName, data: this.stateData, status: 'executing', config: stateConfig });

        try {
            let result;
            switch (stateConfig.Type) {
                case 'Task':
                    result = await this.executeTask(stateName, stateConfig);
                    break;
                case 'Choice':
                    result = this.executeChoice(stateName, stateConfig);
                    break;
                case 'Wait':
                    result = await this.executeWait(stateName, stateConfig);
                    break;
                case 'Pass':
                    result = this.executePass(stateName, stateConfig);
                    break;
                case 'Succeed':
                    result = this.executeSucceed(stateName, stateConfig);
                    break;
                case 'Fail':
                    result = this.executeFail(stateName, stateConfig);
                    break;
                default:
                    throw new Error(`Unsupported state type: ${stateConfig.Type}`);
            }

            const duration = Date.now() - startTime;
            this.addHistory(stateName, `Completed in ${duration}ms`, { result, nextState: result?.nextState });

            if (result?.nextState) {
                this.currentState = result.nextState;
                this.emit('stateChange', { state: this.currentState, data: this.stateData, status: 'entering', previousState: stateName });
            } else if (result?.terminal) {
                this.currentState = null;
                this.isRunning = false;
                this.emit('executionComplete', { success: result.success, data: this.stateData });
            }
        } catch (err) {
            const duration = Date.now() - startTime;
            this.addHistory(stateName, `Failed after ${duration}ms: ${err.message}`, { error: err.message });
            
            // Check for Catch block
            if (stateConfig.Catch) {
                const catcher = stateConfig.Catch.find(c => 
                    c.ErrorEquals.includes('States.ALL') || c.ErrorEquals.includes(err.name)
                );
                if (catcher) {
                    if (catcher.ResultPath) {
                        this.setResultPath(catcher.ResultPath, { Error: err.name, Cause: err.message });
                    }
                    this.currentState = catcher.Next;
                    this.emit('stateChange', { state: this.currentState, data: this.stateData, status: 'entering', caughtError: true });
                    return;
                }
            }
            
            this.emit('error', { state: stateName, error: err });
            this.isRunning = false;
        }
    }

    // Execute a Task state (calls /api/sf/invoke)
    async executeTask(stateName, config) {
        // Build the request parameters
        const params = this.resolveParameters(config.Parameters || {});
        
        // Special handling for WaitForIntervention state
        if (stateName === 'WaitForIntervention') {
            return await this.handleIntervention(stateName, config, params);
        }

        // Call the SF invoke endpoint
        const response = await fetch('/api/sf/invoke', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(params)
        });

        const result = await response.json();

        if (!response.ok) {
            const error = new Error(result.error || 'Task failed');
            error.name = 'TaskFailed';
            throw error;
        }

        // Store result at ResultPath
        if (config.ResultPath) {
            this.setResultPath(config.ResultPath, result);
        } else if (config.ResultPath !== null) {
            // Default: replace entire state data
            this.stateData = result;
        }

        return { nextState: config.Next };
    }

    // Handle WaitForIntervention state (simulates EventBridge waitForTaskToken)
    async handleIntervention(stateName, config, params) {
        this.isPaused = true;
        
        // Extract intervention details from state data
        const interventionInfo = {
            operation_id: this.stateData.operation_id,
            pause_reason: this.stateData.step_result?.pause_reason || 'Intervention required',
            step_name: this.stateData.step_result?.step_name,
            step_index: this.stateData.step_result?.step_index
        };

        this.addHistory(stateName, 'Waiting for intervention', interventionInfo);
        this.emit('stateChange', { 
            state: stateName, 
            data: this.stateData, 
            status: 'waiting_intervention',
            interventionInfo 
        });

        // Wait for intervention response via callback
        return new Promise((resolve) => {
            this.interventionCallback = (response) => {
                this.isPaused = false;
                
                // Store intervention response
                if (config.ResultPath) {
                    this.setResultPath(config.ResultPath, response);
                } else {
                    this.stateData.intervention_response = response;
                }

                this.addHistory(stateName, `Intervention received: ${response.action}`, response);
                resolve({ nextState: config.Next });
            };
        });
    }

    // Provide intervention response (called from UI)
    provideInterventionResponse(response) {
        if (this.interventionCallback) {
            this.interventionCallback(response);
            this.interventionCallback = null;
            
            // Resume execution if not in manual mode
            if (!this.manualMode) {
                this.runToCompletion();
            }
        }
    }

    // Execute a Choice state
    executeChoice(stateName, config) {
        for (const choice of config.Choices) {
            if (this.evaluateChoiceRule(choice)) {
                return { nextState: choice.Next };
            }
        }
        
        if (config.Default) {
            return { nextState: config.Default };
        }
        
        throw new Error('No choice matched and no default specified');
    }

    // Evaluate a choice rule
    evaluateChoiceRule(rule) {
        // Handle And/Or/Not combinators
        if (rule.And) {
            return rule.And.every(r => this.evaluateChoiceRule(r));
        }
        if (rule.Or) {
            return rule.Or.some(r => this.evaluateChoiceRule(r));
        }
        if (rule.Not) {
            return !this.evaluateChoiceRule(rule.Not);
        }

        // Get the value at the variable path
        const value = this.getValueAtPath(rule.Variable);

        // Evaluate comparison
        if ('StringEquals' in rule) return value === rule.StringEquals;
        if ('StringEqualsPath' in rule) return value === this.getValueAtPath(rule.StringEqualsPath);
        if ('NumericEquals' in rule) return value === rule.NumericEquals;
        if ('NumericGreaterThan' in rule) return value > rule.NumericGreaterThan;
        if ('NumericLessThan' in rule) return value < rule.NumericLessThan;
        if ('BooleanEquals' in rule) return value === rule.BooleanEquals;
        if ('IsPresent' in rule) return (value !== undefined && value !== null) === rule.IsPresent;
        if ('IsNull' in rule) return (value === null) === rule.IsNull;

        return false;
    }

    // Execute a Wait state
    async executeWait(stateName, config) {
        let waitSeconds = config.Seconds || 0;
        
        if (config.SecondsPath) {
            waitSeconds = this.getValueAtPath(config.SecondsPath);
        }

        // Apply speed multiplier (min 100ms for visibility)
        const actualWait = Math.max(100, (waitSeconds * 1000) / this.speedMultiplier);
        
        this.addHistory(stateName, `Waiting ${waitSeconds}s (actual: ${Math.round(actualWait)}ms)`, {});
        await this.sleep(actualWait);

        return { nextState: config.Next };
    }

    // Execute a Pass state
    executePass(stateName, config) {
        if (config.Result) {
            if (config.ResultPath) {
                this.setResultPath(config.ResultPath, config.Result);
            } else {
                this.stateData = config.Result;
            }
        }

        if (config.Parameters) {
            const params = this.resolveParameters(config.Parameters);
            if (config.ResultPath) {
                this.setResultPath(config.ResultPath, params);
            } else {
                this.stateData = params;
            }
        }

        return { nextState: config.Next };
    }

    // Execute a Succeed state
    executeSucceed(stateName, config) {
        this.addHistory(stateName, 'Execution succeeded', this.stateData);
        return { terminal: true, success: true };
    }

    // Execute a Fail state
    executeFail(stateName, config) {
        let cause = config.Cause || 'Execution failed';
        
        // Resolve dynamic cause if it uses States.Format
        if (cause.includes('States.Format')) {
            // Simple substitution for our use case
            const match = cause.match(/States\.Format\('([^']+)',\s*\$\.([^)]+)\)/);
            if (match) {
                const template = match[1];
                const path = '$.' + match[2];
                const value = this.getValueAtPath(path);
                cause = template.replace('{}', JSON.stringify(value));
            }
        }

        this.addHistory(stateName, `Execution failed: ${config.Error}`, { error: config.Error, cause });
        return { terminal: true, success: false, error: config.Error, cause };
    }

    // Resolve parameters with JSONPath substitution
    resolveParameters(params) {
        const resolved = {};
        for (const [key, value] of Object.entries(params)) {
            if (key.endsWith('.$')) {
                // JSONPath reference
                const actualKey = key.slice(0, -2);
                resolved[actualKey] = this.resolveValue(value);
            } else if (typeof value === 'object' && value !== null) {
                resolved[key] = this.resolveParameters(value);
            } else {
                resolved[key] = value;
            }
        }
        return resolved;
    }

    // Resolve a single value (could be path or literal)
    resolveValue(value) {
        if (typeof value === 'string' && value.startsWith('$.')) {
            return this.getValueAtPath(value);
        }
        if (typeof value === 'string' && value.startsWith('$$.')) {
            // Context object - return placeholder for demo
            if (value === '$$.Task.Token') {
                return 'demo-task-token-' + Date.now();
            }
            return value;
        }
        return value;
    }

    // Get value at JSONPath
    getValueAtPath(path) {
        if (!path || !path.startsWith('$')) return undefined;
        
        const parts = path.substring(2).split('.');
        let current = this.stateData;
        
        for (const part of parts) {
            if (part === '') continue;
            if (current === undefined || current === null) return undefined;
            current = current[part];
        }
        
        return current;
    }

    // Set value at ResultPath
    setResultPath(path, value) {
        if (path === '$') {
            this.stateData = value;
            return;
        }

        const parts = path.substring(2).split('.');
        let current = this.stateData;
        
        for (let i = 0; i < parts.length - 1; i++) {
            if (parts[i] === '') continue;
            if (!current[parts[i]]) {
                current[parts[i]] = {};
            }
            current = current[parts[i]];
        }
        
        current[parts[parts.length - 1]] = value;
    }

    // Add entry to execution history
    addHistory(state, message, data) {
        const entry = {
            timestamp: new Date().toISOString(),
            state,
            message,
            data
        };
        this.history.push(entry);
        this.emit('historyUpdate', { entry, history: this.history });
    }

    // Pause execution
    pause() {
        this.isPaused = true;
    }

    // Resume execution
    resume() {
        if (this.isPaused && this.isRunning) {
            this.isPaused = false;
            if (!this.manualMode) {
                this.runToCompletion();
            }
        }
    }

    // Stop execution
    stop() {
        this.isRunning = false;
        this.isPaused = false;
        this.interventionCallback = null;
    }

    // Set execution speed (1 = normal, 10 = 10x faster)
    setSpeed(multiplier) {
        this.speedMultiplier = Math.max(0.1, Math.min(100, multiplier));
    }

    // Set manual stepping mode
    setManualMode(enabled) {
        this.manualMode = enabled;
    }

    // Sleep helper
    sleep(ms) {
        return new Promise(resolve => setTimeout(resolve, ms));
    }

    // Get current execution state for UI
    getExecutionState() {
        return {
            isRunning: this.isRunning,
            isPaused: this.isPaused,
            currentState: this.currentState,
            stateData: this.stateData,
            history: this.history,
            manualMode: this.manualMode,
            speedMultiplier: this.speedMultiplier,
            waitingForIntervention: this.interventionCallback !== null
        };
    }
}

// Global instance for the demo
let sfExecutor = null;

function getSFExecutor() {
    if (!sfExecutor) {
        sfExecutor = new StepFunctionExecutor();
    }
    return sfExecutor;
}
