// Step Functions State Machine Visualization
// Renders the ASL state machine as an interactive SVG diagram

class StepFunctionGraph {
    constructor(containerId) {
        this.container = document.getElementById(containerId);
        this.definition = null;
        this.currentState = null;
        this.completedStates = new Set();
        this.failedStates = new Set();
        
        // Layout configuration for our specific state machine
        // Organized in a clear top-to-bottom flow with distinct lanes:
        // - Left lane: Intervention/Rollback path
        // - Center lane: Main happy path
        // - Right lane: Polling/Resume path
        
        const COL_LEFT = 60;      // Rollback lane
        const COL_MID_LEFT = 260; // Intervention lane
        const COL_CENTER = 460;   // Main execution lane
        const COL_RIGHT = 660;    // Polling lane
        const ROW_HEIGHT = 80;    // Vertical spacing between rows
        
        this.statePositions = {
            // Row 0: Entry point
            'ValidateInput':              { x: COL_CENTER, y: 30 },
            
            // Row 1: Initial branching
            'CreateOperation':            { x: COL_MID_LEFT, y: 30 + ROW_HEIGHT },
            'GetOperationStatus':         { x: COL_RIGHT, y: 30 + ROW_HEIGHT },
            
            // Row 2: Setup states
            'ExtractOperationId':         { x: COL_MID_LEFT, y: 30 + ROW_HEIGHT * 2 },
            'CheckExistingOperationState': { x: COL_RIGHT, y: 30 + ROW_HEIGHT * 2 },
            
            // Row 3: Start and main loop entry
            'StartOperation':             { x: COL_CENTER, y: 30 + ROW_HEIGHT * 3 },
            
            // Row 4-5: Main execution loop
            'ExecuteStep':                { x: COL_CENTER, y: 30 + ROW_HEIGHT * 4 },
            'CheckStepResult':            { x: COL_CENTER, y: 30 + ROW_HEIGHT * 5 },
            
            // Row 6: Branch to intervention or polling
            'CheckForIntervention':       { x: COL_MID_LEFT, y: 30 + ROW_HEIGHT * 6 },
            'WaitBeforePoll':             { x: COL_RIGHT, y: 30 + ROW_HEIGHT * 6 },
            
            // Row 7: Intervention waiting / Polling
            'WaitForIntervention':        { x: COL_MID_LEFT, y: 30 + ROW_HEIGHT * 7 },
            'PollWaitCondition':          { x: COL_RIGHT, y: 30 + ROW_HEIGHT * 7 },
            
            // Row 8: Process results
            'ProcessInterventionResponse': { x: COL_MID_LEFT, y: 30 + ROW_HEIGHT * 8 },
            'CheckPollResult':            { x: COL_RIGHT, y: 30 + ROW_HEIGHT * 8 },
            
            // Row 9: Resume action check
            'CheckResumeAction':          { x: COL_MID_LEFT, y: 30 + ROW_HEIGHT * 9 },
            
            // Row 10: Rollback path
            'WaitForRollback':            { x: COL_LEFT, y: 30 + ROW_HEIGHT * 10 },
            
            // Row 11: Rollback check
            'CheckRollbackStatus':        { x: COL_LEFT, y: 30 + ROW_HEIGHT * 11 },
            'IsRollbackComplete':         { x: COL_LEFT, y: 30 + ROW_HEIGHT * 12 },
            
            // Terminal states (Row 10-12, spread horizontally)
            'OperationSucceeded':         { x: COL_CENTER, y: 30 + ROW_HEIGHT * 10 },
            'OperationFailed':            { x: COL_CENTER + 100, y: 30 + ROW_HEIGHT * 11 },
            'OperationAborted':           { x: COL_MID_LEFT, y: 30 + ROW_HEIGHT * 11 },
            'InterventionTimeout':        { x: COL_MID_LEFT - 60, y: 30 + ROW_HEIGHT * 10 },
            'OperationRolledBack':        { x: COL_LEFT, y: 30 + ROW_HEIGHT * 13 }
        };

        this.stateWidth = 170;
        this.stateHeight = 44;
    }

    // Load and render the state machine
    async load() {
        try {
            const response = await fetch('/api/sf/definition');
            this.definition = await response.json();
            this.render();
        } catch (err) {
            console.error('Failed to load SF definition:', err);
            this.container.innerHTML = '<div class="sf-error">Failed to load state machine definition</div>';
        }
    }

    // Render the state machine
    render() {
        if (!this.definition) return;

        const width = 900;
        const height = 1150;

        let svg = `<svg width="100%" height="${height}" viewBox="0 0 ${width} ${height}" class="sf-graph">`;
        
        // Add defs for arrow markers
        svg += `
            <defs>
                <marker id="arrowhead" markerWidth="10" markerHeight="7" refX="9" refY="3.5" orient="auto">
                    <polygon points="0 0, 10 3.5, 0 7" fill="var(--muted-foreground)" />
                </marker>
                <marker id="arrowhead-active" markerWidth="10" markerHeight="7" refX="9" refY="3.5" orient="auto">
                    <polygon points="0 0, 10 3.5, 0 7" fill="var(--blue)" />
                </marker>
                <marker id="arrowhead-success" markerWidth="10" markerHeight="7" refX="9" refY="3.5" orient="auto">
                    <polygon points="0 0, 10 3.5, 0 7" fill="var(--green)" />
                </marker>
            </defs>
        `;

        // Draw connections first (behind states)
        svg += this.renderConnections();

        // Draw states
        for (const [name, config] of Object.entries(this.definition.States)) {
            svg += this.renderState(name, config);
        }

        svg += '</svg>';
        this.container.innerHTML = svg;
    }

    // Render all connections between states
    renderConnections() {
        let connections = '';
        
        for (const [name, config] of Object.entries(this.definition.States)) {
            const from = this.statePositions[name];
            if (!from) continue;

            // Direct Next transition
            if (config.Next) {
                connections += this.renderConnection(name, config.Next);
            }

            // Choice transitions
            if (config.Choices) {
                for (const choice of config.Choices) {
                    if (choice.Next) {
                        connections += this.renderConnection(name, choice.Next, 'choice');
                    }
                }
            }

            // Default transition
            if (config.Default) {
                connections += this.renderConnection(name, config.Default, 'default');
            }

            // Catch transitions
            if (config.Catch) {
                for (const c of config.Catch) {
                    connections += this.renderConnection(name, c.Next, 'catch');
                }
            }
        }

        return connections;
    }

    // Render a single connection
    renderConnection(fromState, toState, type = 'normal') {
        const from = this.statePositions[fromState];
        const to = this.statePositions[toState];
        
        if (!from || !to) return '';

        // Get state configs to check types
        const fromConfig = this.definition.States[fromState];
        const toConfig = this.definition.States[toState];
        
        const w = this.stateWidth;
        const h = this.stateHeight;
        
        // Calculate connection points based on state types and relative positions
        let fromX, fromY, toX, toY;
        
        // Default: bottom of from state
        fromX = from.x + w / 2;
        fromY = from.y + h;
        
        // Default: top of to state  
        toX = to.x + w / 2;
        toY = to.y;
        
        // For Choice states (diamonds), exit from sides if target is to the left/right
        if (fromConfig?.Type === 'Choice') {
            if (to.x < from.x - 50) {
                // Target is to the left - exit from left side
                fromX = from.x + w/2 - w * 0.3;
                fromY = from.y + h/2;
            } else if (to.x > from.x + 50) {
                // Target is to the right - exit from right side
                fromX = from.x + w/2 + w * 0.3;
                fromY = from.y + h/2;
            }
        }
        
        // If going upward, enter from bottom of target
        if (to.y < from.y) {
            toY = to.y + h;
        }

        // Determine if this connection was traversed
        const isCompleted = this.completedStates.has(fromState) && 
                           (this.completedStates.has(toState) || this.currentState === toState);
        
        let pathClass = 'sf-connection';
        let marker = 'url(#arrowhead)';
        
        if (type === 'catch') {
            pathClass += ' sf-connection-catch';
        } else if (type === 'choice') {
            pathClass += ' sf-connection-choice';
        }
        
        if (isCompleted) {
            pathClass += ' sf-connection-completed';
            marker = 'url(#arrowhead-success)';
        }

        // Calculate path with smart routing
        let path;
        const dx = toX - fromX;
        const dy = toY - fromY;
        
        if (Math.abs(dx) < 15 && dy > 0) {
            // Nearly straight vertical line going down
            path = `M ${fromX} ${fromY} L ${toX} ${toY - 8}`;
        } else if (dy < 0) {
            // Going upward - route around
            const offsetX = dx > 0 ? 30 : -30;
            path = `M ${fromX} ${fromY} L ${fromX} ${fromY + 15} L ${fromX + offsetX} ${fromY + 15} L ${toX + offsetX} ${toY - 15} L ${toX} ${toY - 15} L ${toX} ${toY + 8}`;
        } else {
            // Curved path for diagonal connections
            const midY = fromY + dy * 0.5;
            path = `M ${fromX} ${fromY} C ${fromX} ${midY}, ${toX} ${midY}, ${toX} ${toY - 8}`;
        }

        return `<path d="${path}" class="${pathClass}" marker-end="${marker}" />`;
    }

    // Render a single state
    renderState(name, config) {
        const pos = this.statePositions[name];
        if (!pos) {
            console.warn('No position for state:', name);
            return '';
        }

        const x = pos.x;
        const y = pos.y;
        const w = this.stateWidth;
        const h = this.stateHeight;

        // Determine state status
        const isCurrent = this.currentState === name;
        const isCompleted = this.completedStates.has(name);
        const isFailed = this.failedStates.has(name);
        const isActive = isCurrent || isCompleted || isFailed;

        // Build state class
        let stateClass = 'sf-state';
        if (isCurrent) {
            stateClass += ' sf-state-current';
        } else if (isCompleted) {
            stateClass += ' sf-state-completed';
        } else if (isFailed) {
            stateClass += ' sf-state-failed';
        }

        // Different shapes for different state types
        let shape;
        const typeClass = `sf-state-type-${config.Type.toLowerCase()}`;
        stateClass += ` ${typeClass}`;

        if (config.Type === 'Choice') {
            // Diamond shape for Choice - make it wider
            const cx = x + w/2;
            const cy = y + h/2;
            const dw = w * 0.6;  // Diamond width
            const dh = h;        // Diamond height
            shape = `<polygon points="${cx},${y} ${cx+dw/2},${cy} ${cx},${y+h} ${cx-dw/2},${cy}" class="${stateClass}" />`;
        } else if (config.Type === 'Succeed') {
            // Rounded rectangle with thicker border for terminal states
            shape = `<rect x="${x}" y="${y}" width="${w}" height="${h}" rx="22" class="${stateClass} sf-state-terminal sf-state-succeed" />`;
        } else if (config.Type === 'Fail') {
            // Rounded rectangle for fail states
            shape = `<rect x="${x}" y="${y}" width="${w}" height="${h}" rx="22" class="${stateClass} sf-state-terminal sf-state-fail" />`;
        } else {
            // Regular rectangle for other states
            const rx = config.Type === 'Wait' ? 6 : 8;
            shape = `<rect x="${x}" y="${y}" width="${w}" height="${h}" rx="${rx}" class="${stateClass}" />`;
        }

        // State label - brighter when active
        const labelY = y + h/2 + 4;
        const labelClass = isActive ? 'sf-state-label sf-state-label-active' : 'sf-state-label';
        // Truncate long names
        let displayName = name;
        if (name.length > 22) {
            displayName = name.substring(0, 20) + '...';
        }
        const label = `<text x="${x + w/2}" y="${labelY}" class="${labelClass}">${displayName}</text>`;

        // Type indicator - only show for active states or on hover via CSS
        const typeClass2 = isActive ? 'sf-state-type sf-state-type-active' : 'sf-state-type';
        const typeLabel = `<text x="${x + w/2}" y="${y - 6}" class="${typeClass2}">${config.Type}</text>`;

        return `<g class="sf-state-group" data-state="${name}">
            ${shape}
            ${label}
            ${typeLabel}
        </g>`;
    }

    // Update the current state highlight
    setCurrentState(stateName) {
        if (this.currentState && this.currentState !== stateName) {
            this.completedStates.add(this.currentState);
        }
        this.currentState = stateName;
        this.render();
    }

    // Mark a state as completed
    markCompleted(stateName) {
        this.completedStates.add(stateName);
        this.render();
    }

    // Mark a state as failed
    markFailed(stateName) {
        this.failedStates.add(stateName);
        this.render();
    }

    // Reset all state highlights
    reset() {
        this.currentState = null;
        this.completedStates.clear();
        this.failedStates.clear();
        this.render();
    }

    // Scroll the container to show the current state
    scrollToState(stateName) {
        const pos = this.statePositions[stateName];
        if (!pos) return;

        const container = this.container.parentElement;
        if (container && container.scrollHeight > container.clientHeight) {
            const targetY = pos.y - container.clientHeight / 2 + this.stateHeight / 2;
            container.scrollTo({ top: targetY, behavior: 'smooth' });
        }
    }
}

// UI controller for the Step Functions demo tab
class StepFunctionDemoUI {
    constructor() {
        this.executor = getSFExecutor();
        this.graph = null;
        this.selectedCluster = null;
        this.selectedRegion = null;
        this.operationType = '';
        this.operationParams = {};
        // Cluster metadata (fetched when cluster is selected)
        this.clusterInfo = null;
        this.upgradeTargets = null;
        this.instanceTypes = null;
    }

    // Initialize the UI
    async init() {
        // Initialize the graph
        this.graph = new StepFunctionGraph('sf-graph-container');
        await this.graph.load();

        // Set up executor event listeners
        this.executor.on('stateChange', (data) => this.onStateChange(data));
        this.executor.on('historyUpdate', (data) => this.onHistoryUpdate(data));
        this.executor.on('executionComplete', (data) => this.onExecutionComplete(data));
        this.executor.on('error', (data) => this.onError(data));

        // Set up UI event handlers
        this.setupEventHandlers();
        
        // Load regions for selection (cascades to clusters)
        await this.loadRegions();
    }

    // Set up event handlers for UI controls
    setupEventHandlers() {
        // Run button
        const runBtn = document.getElementById('sf-run-btn');
        if (runBtn) {
            runBtn.addEventListener('click', () => this.startExecution());
        }

        // Step button
        const stepBtn = document.getElementById('sf-step-btn');
        if (stepBtn) {
            stepBtn.addEventListener('click', () => this.stepExecution());
        }

        // Pause button
        const pauseBtn = document.getElementById('sf-pause-btn');
        if (pauseBtn) {
            pauseBtn.addEventListener('click', () => this.togglePause());
        }

        // Reset button
        const resetBtn = document.getElementById('sf-reset-btn');
        if (resetBtn) {
            resetBtn.addEventListener('click', () => this.resetExecution());
        }

        // Speed slider
        const speedSlider = document.getElementById('sf-speed-slider');
        if (speedSlider) {
            speedSlider.addEventListener('input', (e) => {
                const speed = parseFloat(e.target.value);
                this.executor.setSpeed(speed);
                document.getElementById('sf-speed-value').textContent = speed + 'x';
            });
            // Set initial speed from slider value
            this.executor.setSpeed(parseFloat(speedSlider.value));
        }

        // Manual mode toggle
        const manualToggle = document.getElementById('sf-manual-mode');
        if (manualToggle) {
            manualToggle.addEventListener('change', (e) => {
                this.executor.setManualMode(e.target.checked);
                this.updateControlsState();
            });
        }

        // Region selector - cascades to cluster loading
        const regionSelect = document.getElementById('sf-region-select');
        if (regionSelect) {
            regionSelect.addEventListener('change', (e) => {
                this.selectedRegion = e.target.value;
                if (this.selectedRegion) {
                    this.loadClusters(this.selectedRegion);
                } else {
                    // Reset cluster dropdown
                    this.resetClusterDropdown();
                }
            });
        }

        // Cluster selector - cascades to cluster info fetching
        const clusterSelect = document.getElementById('sf-cluster-select');
        if (clusterSelect) {
            clusterSelect.addEventListener('change', (e) => {
                this.selectedCluster = e.target.value;
                if (this.selectedCluster) {
                    this.fetchClusterInfo(this.selectedCluster, this.selectedRegion);
                } else {
                    // Reset operation type dropdown
                    this.setOperationTypeEnabled(false);
                }
            });
        }

        // Operation type selector
        const opTypeSelect = document.getElementById('sf-operation-type');
        if (opTypeSelect) {
            opTypeSelect.addEventListener('change', (e) => {
                this.operationType = e.target.value;
                this.updateParamsUI();
            });
        }
    }

    // Helper to truncate text with ellipsis
    truncateText(text, maxLength) {
        if (text.length <= maxLength) return text;
        return text.substring(0, maxLength - 3) + '...';
    }

    // Update a custom select dropdown
    updateCustomSelect(selectId, options, selectedValue, placeholder) {
        const container = document.querySelector(`[data-select-id="${selectId}"]`);
        if (!container) return;
        
        const trigger = container.querySelector('.custom-select-trigger');
        const dropdown = container.querySelector('.custom-select-dropdown');
        const hiddenSelect = container.querySelector('select');
        
        // Build options HTML
        let optionsHtml = '';
        let selectedText = placeholder;
        let selectedDisplay = placeholder;
        
        options.forEach(opt => {
            const isSelected = opt.value === selectedValue;
            if (isSelected && opt.value) {
                selectedText = opt.text;
                selectedDisplay = opt.display || opt.text;
            }
            const displayAttr = opt.display ? ` data-display="${opt.display}"` : '';
            const content = opt.html || opt.text;
            optionsHtml += `<div class="custom-select-option${isSelected ? ' selected' : ''}" data-value="${opt.value}"${displayAttr} role="option">${content}</div>`;
        });
        
        // Update dropdown
        dropdown.innerHTML = optionsHtml;
        
        // Update trigger with display text
        trigger.querySelector('span').textContent = selectedDisplay;
        trigger.classList.toggle('placeholder', !selectedValue);
        
        // Update hidden select
        hiddenSelect.innerHTML = options.map(opt => 
            `<option value="${opt.value}"${opt.value === selectedValue ? ' selected' : ''}>${opt.text}</option>`
        ).join('');
    }

    // Load regions from the API
    async loadRegions() {
        try {
            const response = await fetch('/api/regions');
            const data = await response.json();
            
            const options = [
                { value: '', text: 'Select region...' },
                ...data.regions.map(r => ({ value: r, text: r }))
            ];
            
            this.updateCustomSelect('sf-region-select', options, data.default_region, 'Select region...');
            
            // Enable trigger
            const trigger = document.querySelector('[data-select-id="sf-region-select"] .custom-select-trigger');
            if (trigger) {
                trigger.disabled = false;
                trigger.removeAttribute('disabled');
            }
            
            // Auto-load clusters if default region exists
            if (data.default_region) {
                this.selectedRegion = data.default_region;
                await this.loadClusters(data.default_region);
            }
        } catch (err) {
            console.error('Failed to load regions:', err);
            this.updateCustomSelect('sf-region-select', [{ value: '', text: 'Failed to load regions' }], '', 'Failed to load regions');
        }
    }

    // Load clusters for a specific region
    async loadClusters(region) {
        const trigger = document.querySelector('[data-select-id="sf-cluster-select"] .custom-select-trigger');
        const loading = document.getElementById('sf-cluster-loading');
        
        // Disable and show loading state
        if (trigger) {
            trigger.disabled = true;
            trigger.setAttribute('disabled', '');
        }
        this.updateCustomSelect('sf-cluster-select', [{ value: '', text: 'Loading clusters...' }], '', 'Loading clusters...');
        if (loading) loading.style.display = 'block';
        
        // Reset cluster selection and dependent dropdowns
        this.selectedCluster = null;
        this.setOperationTypeEnabled(false);
        
        try {
            const response = await fetch(`/api/regions/${region}/clusters`);
            const clusters = await response.json();
            
            if (clusters.length === 0) {
                this.updateCustomSelect('sf-cluster-select', [{ value: '', text: 'No Aurora clusters found' }], '', 'No Aurora clusters found');
            } else {
                const options = [
                    { value: '', text: 'Select cluster...' },
                    ...clusters.map(c => ({
                        value: c.cluster_id, 
                        text: `${c.cluster_id} (${c.engine} ${c.engine_version})`,
                        display: this.truncateText(c.cluster_id, 40),
                        html: `<div><strong>${this.truncateText(c.cluster_id, 40)}</strong><div style="font-size: 11px; color: var(--muted-foreground);">${c.engine} ${c.engine_version}</div></div>`
                    }))
                ];
                this.updateCustomSelect('sf-cluster-select', options, '', 'Select cluster...');
                
                // Enable trigger
                if (trigger) {
                    trigger.disabled = false;
                    trigger.removeAttribute('disabled');
                }
            }
        } catch (err) {
            console.error('Failed to load clusters:', err);
            this.updateCustomSelect('sf-cluster-select', [{ value: '', text: 'Failed to load clusters' }], '', 'Failed to load clusters');
        }
        
        if (loading) loading.style.display = 'none';
    }

    // Reset cluster dropdown to initial state
    resetClusterDropdown() {
        this.selectedCluster = null;
        this.updateCustomSelect('sf-cluster-select', [{ value: '', text: 'Select a region first...' }], '', 'Select a region first...');
        const trigger = document.querySelector('[data-select-id="sf-cluster-select"] .custom-select-trigger');
        if (trigger) {
            trigger.disabled = true;
            trigger.setAttribute('disabled', '');
        }
        this.setOperationTypeEnabled(false);
    }

    // Enable/disable operation type dropdown
    setOperationTypeEnabled(enabled) {
        const trigger = document.querySelector('[data-select-id="sf-operation-type"] .custom-select-trigger');
        const hiddenSelect = document.getElementById('sf-operation-type');
        
        if (enabled) {
            if (trigger) {
                trigger.disabled = false;
                trigger.removeAttribute('disabled');
                trigger.querySelector('span').textContent = 'Select operation...';
                trigger.classList.add('placeholder');
            }
            if (hiddenSelect) {
                hiddenSelect.disabled = false;
                hiddenSelect.value = '';
            }
            this.operationType = '';
        } else {
            if (trigger) {
                trigger.disabled = true;
                trigger.setAttribute('disabled', '');
                trigger.querySelector('span').textContent = 'Select a cluster first...';
                trigger.classList.add('placeholder');
            }
            if (hiddenSelect) {
                hiddenSelect.disabled = true;
                hiddenSelect.value = '';
            }
            this.operationType = '';
            // Clear params container
            const paramsContainer = document.getElementById('sf-params-container');
            if (paramsContainer) paramsContainer.innerHTML = '';
        }
    }

    // Fetch cluster info when cluster is selected
    async fetchClusterInfo(clusterId, region) {
        this.clusterInfo = null;
        this.upgradeTargets = null;
        this.instanceTypes = null;
        
        if (!clusterId) {
            this.setOperationTypeEnabled(false);
            return;
        }
        
        try {
            const headers = { 'X-Cluster-Id': clusterId };
            if (region) headers['X-Region'] = region;
            
            // Fetch cluster info, upgrade targets, and instance types in parallel
            const [clusterResponse, upgradeResponse, instanceTypesResponse] = await Promise.all([
                fetch('/api/cluster', { headers }),
                fetch('/api/cluster/upgrade-targets', { headers }),
                fetch('/api/cluster/instance-types', { headers })
            ]);
            
            if (clusterResponse.ok) {
                this.clusterInfo = await clusterResponse.json();
            }
            
            if (upgradeResponse.ok) {
                this.upgradeTargets = await upgradeResponse.json();
            } else {
                console.error('Failed to fetch upgrade targets:', upgradeResponse.status);
                this.upgradeTargets = { upgrade_targets: [], error: true };
            }
            
            if (instanceTypesResponse.ok) {
                this.instanceTypes = await instanceTypesResponse.json();
            } else {
                console.error('Failed to fetch instance types:', instanceTypesResponse.status);
                this.instanceTypes = { instance_types: [], error: true };
            }
            
            // Enable operation type dropdown
            this.setOperationTypeEnabled(true);
            
            // Re-render params if operation type is already selected
            if (this.operationType) {
                this.updateParamsUI();
            }
        } catch (err) {
            console.error('Failed to fetch cluster info:', err);
        }
    }

    // Get writer instance from current cluster info
    getWriterInstance() {
        if (!this.clusterInfo || !this.clusterInfo.instances) return null;
        return this.clusterInfo.instances.find(i => i.role === 'writer');
    }

    // Update params UI based on operation type
    updateParamsUI() {
        const container = document.getElementById('sf-params-container');
        if (!container) return;

        if (this.operationType === 'engine_upgrade') {
            // Add dropdown for engine version
            container.innerHTML = `
                <div class="form-group">
                    <label>Target Engine Version</label>
                    <div class="custom-select" data-select-id="sf-param-engine-version">
                        <button type="button" class="custom-select-trigger" aria-haspopup="listbox" disabled>
                            <span>Loading versions...</span>
                            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m6 9 6 6 6-6"/></svg>
                        </button>
                        <div class="custom-select-dropdown" role="listbox"></div>
                        <select id="sf-param-engine-version" tabindex="-1" aria-hidden="true"></select>
                    </div>
                </div>
                <div style="display: flex; gap: 10px; padding: 10px 12px; border: 1px solid var(--blue); border-radius: 8px; background: var(--blue-muted); margin-top: 12px; margin-bottom: 8px;">
                    <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--blue)" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="flex-shrink: 0; margin-top: 2px;"><circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/></svg>
                    <div style="font-size: 13px; color: var(--text-primary); line-height: 1.4;">This operation uses AWS Blue-Green Deployment for near-zero-downtime upgrades.</div>
                </div>`;
            this.populateEngineVersionDropdown();
        } else if (this.operationType === 'instance_type_change') {
            // Add dropdown for instance type
            container.innerHTML = `
                <div class="form-group">
                    <label>Target Instance Type</label>
                    <div class="custom-select" data-select-id="sf-param-instance-type">
                        <button type="button" class="custom-select-trigger" aria-haspopup="listbox" disabled>
                            <span>Loading instance types...</span>
                            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m6 9 6 6 6-6"/></svg>
                        </button>
                        <div class="custom-select-dropdown" role="listbox"></div>
                        <select id="sf-param-instance-type" tabindex="-1" aria-hidden="true"></select>
                    </div>
                </div>`;
            this.populateInstanceTypeDropdown();
        } else if (this.operationType === 'instance_cycle') {
            // No parameters needed, just show info
            container.innerHTML = `
                <div style="display: flex; gap: 10px; padding: 10px 12px; border: 1px solid var(--blue); border-radius: 8px; background: var(--blue-muted); margin-bottom: 8px;">
                    <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--blue)" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="flex-shrink: 0; margin-top: 2px;"><circle cx="12" cy="12" r="10"/><path d="M12 16v-4"/><path d="M12 8h.01"/></svg>
                    <div style="font-size: 13px; color: var(--text-primary); line-height: 1.4;">This operation will reboot all non-autoscaled instances in the cluster one at a time, starting with readers and ending with the writer.</div>
                </div>`;
        } else {
            container.innerHTML = '';
        }
    }

    // Populate engine version dropdown from upgrade targets
    populateEngineVersionDropdown() {
        const selectContainer = document.querySelector('[data-select-id="sf-param-engine-version"]');
        if (!selectContainer) return;

        const trigger = selectContainer.querySelector('.custom-select-trigger');
        const dropdown = selectContainer.querySelector('.custom-select-dropdown');
        const hiddenSelect = selectContainer.querySelector('select');

        if (!this.upgradeTargets || !this.upgradeTargets.upgrade_targets) {
            if (this.upgradeTargets && this.upgradeTargets.error) {
                trigger.querySelector('span').textContent = 'Failed to load versions';
            }
            return;
        }

        const targets = this.upgradeTargets.upgrade_targets;

        if (targets.length === 0) {
            trigger.querySelector('span').textContent = 'No upgrades available';
            return;
        }

        // Group by major/minor upgrades
        const minorUpgrades = targets.filter(t => !t.is_major_version_upgrade);
        const majorUpgrades = targets.filter(t => t.is_major_version_upgrade);

        dropdown.innerHTML = '';
        hiddenSelect.innerHTML = '';

        // Add minor upgrades
        if (minorUpgrades.length > 0) {
            const minorHeader = document.createElement('div');
            minorHeader.className = 'custom-select-group-header';
            minorHeader.textContent = 'Minor Upgrades';
            minorHeader.style.cssText = 'padding: 6px 12px; font-size: 10px; text-transform: uppercase; color: var(--muted-foreground); font-weight: 600; letter-spacing: 0.05em;';
            dropdown.appendChild(minorHeader);

            minorUpgrades.forEach(t => {
                const opt = document.createElement('div');
                opt.className = 'custom-select-option';
                opt.dataset.value = t.engine_version;
                opt.setAttribute('role', 'option');
                opt.textContent = t.engine_version;
                dropdown.appendChild(opt);

                const selectOpt = document.createElement('option');
                selectOpt.value = t.engine_version;
                selectOpt.textContent = t.engine_version;
                hiddenSelect.appendChild(selectOpt);
            });
        }

        // Add major upgrades
        if (majorUpgrades.length > 0) {
            const majorHeader = document.createElement('div');
            majorHeader.className = 'custom-select-group-header';
            majorHeader.textContent = 'Major Upgrades';
            majorHeader.style.cssText = 'padding: 6px 12px; font-size: 10px; text-transform: uppercase; color: var(--muted-foreground); font-weight: 600; letter-spacing: 0.05em;' +
                (minorUpgrades.length > 0 ? ' margin-top: 8px; border-top: 1px solid var(--border); padding-top: 12px;' : '');
            dropdown.appendChild(majorHeader);

            majorUpgrades.forEach(t => {
                const opt = document.createElement('div');
                opt.className = 'custom-select-option';
                opt.dataset.value = t.engine_version;
                opt.setAttribute('role', 'option');
                opt.textContent = t.engine_version;
                dropdown.appendChild(opt);

                const selectOpt = document.createElement('option');
                selectOpt.value = t.engine_version;
                selectOpt.textContent = t.engine_version;
                hiddenSelect.appendChild(selectOpt);
            });
        }

        // Enable trigger
        trigger.disabled = false;
        trigger.removeAttribute('disabled');
        trigger.querySelector('span').textContent = 'Select version...';
        trigger.classList.add('placeholder');
    }

    // Populate instance type dropdown from available instance types
    populateInstanceTypeDropdown() {
        const selectContainer = document.querySelector('[data-select-id="sf-param-instance-type"]');
        if (!selectContainer) return;

        const trigger = selectContainer.querySelector('.custom-select-trigger');
        const dropdown = selectContainer.querySelector('.custom-select-dropdown');
        const hiddenSelect = selectContainer.querySelector('select');
        const writer = this.getWriterInstance();
        const currentType = writer?.instance_type || '';

        if (!this.instanceTypes || !this.instanceTypes.instance_types) {
            if (this.instanceTypes && this.instanceTypes.error) {
                trigger.querySelector('span').textContent = 'Failed to load types';
            }
            return;
        }

        const instanceTypes = this.instanceTypes.instance_types;

        if (instanceTypes.length === 0) {
            trigger.querySelector('span').textContent = 'No instance types available';
            return;
        }

        // Group instance types by family (e.g., r6g, r5, t3)
        const families = {};
        instanceTypes.forEach(t => {
            const parts = t.instance_class.replace('db.', '').split('.');
            const family = parts[0] || 'other';
            if (!families[family]) families[family] = [];
            families[family].push(t);
        });

        dropdown.innerHTML = '';
        hiddenSelect.innerHTML = '';

        // Sort families: by type priority (r, x, m, t), then by generation (newest first)
        const typePriority = { r: 0, x: 1, m: 2, t: 3 };
        const sortedFamilies = Object.keys(families).sort((a, b) => {
            // Extract type letter and generation (e.g., "r6g" -> type="r", gen="6g")
            const typeA = a.charAt(0);
            const typeB = b.charAt(0);
            const genA = a.substring(1);
            const genB = b.substring(1);
            
            // First sort by type priority
            const prioA = typePriority[typeA] ?? 99;
            const prioB = typePriority[typeB] ?? 99;
            if (prioA !== prioB) return prioA - prioB;
            
            // Then sort by generation (descending - newer/higher first)
            // Extract numeric part for comparison (e.g., "6g" -> 6, "6gd" -> 6)
            const numA = parseInt(genA) || 0;
            const numB = parseInt(genB) || 0;
            if (numA !== numB) return numB - numA;
            
            // Finally sort alphabetically for same generation (e.g., r6g before r6gd, r6i)
            return a.localeCompare(b);
        });

        // Size order for sorting within families
        const sizeOrder = ['micro', 'small', 'medium', 'large', 'xlarge', '2xlarge', '4xlarge', '8xlarge', '12xlarge', '16xlarge', '24xlarge', '32xlarge', '48xlarge'];

        let isFirst = true;
        sortedFamilies.forEach(family => {
            const types = families[family];
            
            // Sort types by size within family
            types.sort((a, b) => {
                const sizeA = a.instance_class.replace('db.', '').split('.')[1] || '';
                const sizeB = b.instance_class.replace('db.', '').split('.')[1] || '';
                const orderA = sizeOrder.indexOf(sizeA);
                const orderB = sizeOrder.indexOf(sizeB);
                if (orderA === -1 && orderB === -1) return sizeA.localeCompare(sizeB);
                if (orderA === -1) return 1;
                if (orderB === -1) return -1;
                return orderA - orderB;
            });
            
            const isNvmeFamily = /d$/.test(family);

            // Add family header
            const header = document.createElement('div');
            header.className = 'custom-select-group-header';
            header.style.cssText = 'padding: 6px 12px; font-size: 10px; text-transform: uppercase; color: var(--muted-foreground); font-weight: 600; letter-spacing: 0.05em;' +
                (!isFirst ? ' margin-top: 8px; border-top: 1px solid var(--border); padding-top: 12px;' : '');
            if (isNvmeFamily) {
                header.innerHTML = family.toUpperCase() + ' <span style="text-transform: none; font-weight: 500; color: var(--blue);">(read optimized)</span>';
            } else {
                header.textContent = family.toUpperCase();
            }
            dropdown.appendChild(header);
            isFirst = false;

            types.forEach(t => {
                const isCurrent = t.instance_class === currentType;

                const opt = document.createElement('div');
                opt.className = 'custom-select-option' + (isCurrent ? ' current-indicator' : '');
                opt.dataset.value = t.instance_class;
                opt.setAttribute('role', 'option');

                if (isCurrent) {
                    opt.innerHTML = t.instance_class + '<span style="font-size: 11px; color: var(--blue); margin-left: 8px;">(current)</span>';
                } else {
                    opt.textContent = t.instance_class;
                }
                dropdown.appendChild(opt);

                const selectOpt = document.createElement('option');
                selectOpt.value = t.instance_class;
                selectOpt.textContent = t.instance_class;
                hiddenSelect.appendChild(selectOpt);
            });
        });

        // Enable trigger and set placeholder
        trigger.disabled = false;
        trigger.removeAttribute('disabled');
        trigger.querySelector('span').textContent = currentType ? `Current: ${currentType}` : 'Select instance type...';
        trigger.classList.add('placeholder');
    }

    // Start a new execution
    async startExecution() {
        if (!this.selectedCluster) {
            showAlertDialog('Please select a cluster first');
            return;
        }

        if (!this.operationType) {
            showAlertDialog('Please select an operation type');
            return;
        }

        // Build operation parameters
        const params = {
            cluster_id: this.selectedCluster,
            region: this.selectedRegion,
            type: this.operationType
        };

        if (this.operationType === 'engine_upgrade') {
            const version = document.getElementById('sf-param-engine-version')?.value;
            if (!version) {
                showAlertDialog('Please select a target engine version');
                return;
            }
            params.params = { target_engine_version: version };
        } else if (this.operationType === 'instance_type_change') {
            const instanceType = document.getElementById('sf-param-instance-type')?.value;
            if (!instanceType) {
                showAlertDialog('Please select a target instance type');
                return;
            }
            params.params = { target_instance_type: instanceType };
        }

        // Reset visualization
        this.graph.reset();
        this.clearHistory();

        // Start execution
        try {
            await this.executor.start({ params });
            this.updateControlsState();
        } catch (err) {
            showAlertDialog('Failed to start execution: ' + err.message);
        }
    }

    // Execute a single step
    async stepExecution() {
        if (!this.executor.isRunning) {
            showAlertDialog('No execution in progress. Click Run to start.');
            return;
        }
        await this.executor.step();
        this.updateControlsState();
    }

    // Toggle pause/resume
    togglePause() {
        const state = this.executor.getExecutionState();
        if (state.isPaused) {
            this.executor.resume();
        } else {
            this.executor.pause();
        }
        this.updateControlsState();
    }

    // Reset execution
    resetExecution() {
        this.executor.stop();
        this.graph.reset();
        this.clearHistory();
        this.updateControlsState();
        this.updateStateDetails(null);
    }

    // Handle state change events
    onStateChange(data) {
        this.graph.setCurrentState(data.state);
        this.graph.scrollToState(data.state);
        this.updateStateDetails(data);
        this.updateControlsState();

        // Handle intervention waiting
        if (data.status === 'waiting_intervention') {
            this.showInterventionUI(data.interventionInfo);
        }
    }

    // Handle history updates
    onHistoryUpdate(data) {
        this.addHistoryEntry(data.entry);
    }

    // Handle execution completion
    onExecutionComplete(data) {
        this.updateControlsState();
        
        const status = data.success ? 'succeeded' : 'failed';
        this.addHistoryEntry({
            timestamp: new Date().toISOString(),
            state: 'ExecutionComplete',
            message: `Execution ${status}`,
            data: data
        });
    }

    // Handle errors
    onError(data) {
        console.error('SF Executor error:', data);
        this.graph.markFailed(data.state);
        this.updateControlsState();
    }

    // Update UI control states
    updateControlsState() {
        const state = this.executor.getExecutionState();
        
        const runBtn = document.getElementById('sf-run-btn');
        const stepBtn = document.getElementById('sf-step-btn');
        const pauseBtn = document.getElementById('sf-pause-btn');
        const resetBtn = document.getElementById('sf-reset-btn');

        if (runBtn) {
            runBtn.disabled = state.isRunning;
            runBtn.textContent = state.isRunning ? 'Running...' : 'Run';
        }

        if (stepBtn) {
            stepBtn.disabled = !state.isRunning || !state.manualMode || state.waitingForIntervention;
        }

        if (pauseBtn) {
            pauseBtn.disabled = !state.isRunning || state.manualMode;
            pauseBtn.textContent = state.isPaused ? 'Resume' : 'Pause';
        }

        if (resetBtn) {
            resetBtn.disabled = false;
        }

        // Update status indicator
        const statusEl = document.getElementById('sf-execution-status');
        if (statusEl) {
            let status = 'Idle';
            let statusClass = '';
            
            if (state.isRunning) {
                if (state.waitingForIntervention) {
                    status = 'Waiting for Intervention';
                    statusClass = 'sf-status-intervention';
                } else if (state.isPaused) {
                    status = 'Paused';
                    statusClass = 'sf-status-paused';
                } else {
                    status = 'Running';
                    statusClass = 'sf-status-running';
                }
            }
            
            statusEl.textContent = status;
            statusEl.className = 'sf-execution-status ' + statusClass;
        }
    }

    // Update state details panel
    updateStateDetails(data) {
        const container = document.getElementById('sf-state-details');
        if (!container) return;

        if (!data) {
            container.innerHTML = '<div class="empty-state">No state selected</div>';
            return;
        }

        container.innerHTML = `
            <div class="sf-detail-row">
                <span class="sf-detail-label">State:</span>
                <span class="sf-detail-value">${data.state}</span>
            </div>
            <div class="sf-detail-row">
                <span class="sf-detail-label">Status:</span>
                <span class="sf-detail-value sf-status-${data.status}">${data.status}</span>
            </div>
            <div class="sf-detail-row">
                <span class="sf-detail-label">Data:</span>
                <pre class="sf-detail-json">${JSON.stringify(data.data, null, 2)}</pre>
            </div>
        `;
    }

    // Add entry to history list
    addHistoryEntry(entry) {
        const list = document.getElementById('sf-history-list');
        if (!list) return;

        // Remove empty state message if present
        const emptyState = list.querySelector('.empty-state');
        if (emptyState) emptyState.remove();

        const time = new Date(entry.timestamp).toLocaleTimeString();
        const html = `
            <div class="sf-history-entry">
                <span class="sf-history-time">${time}</span>
                <span class="sf-history-state">${entry.state}</span>
                <span class="sf-history-message">${entry.message}</span>
            </div>
        `;

        list.insertAdjacentHTML('afterbegin', html);

        // Keep only last 50 entries
        while (list.children.length > 50) {
            list.removeChild(list.lastChild);
        }
    }

    // Clear history list
    clearHistory() {
        const list = document.getElementById('sf-history-list');
        if (list) {
            list.innerHTML = '<div class="empty-state">No execution history</div>';
        }
    }

    // Show intervention UI
    showInterventionUI(info) {
        const container = document.getElementById('sf-intervention-panel');
        if (!container) return;

        container.style.display = 'block';
        container.innerHTML = `
            <div class="sf-intervention-content">
                <h4>Intervention Required</h4>
                <p><strong>Reason:</strong> ${info.pause_reason || 'Manual intervention needed'}</p>
                ${info.step_name ? `<p><strong>Step:</strong> ${info.step_name}</p>` : ''}
                <div class="sf-intervention-actions">
                    <button class="btn-success" onclick="sfDemoUI.provideIntervention('continue')">Continue</button>
                    <button class="btn-warning" onclick="sfDemoUI.provideIntervention('rollback')">Rollback</button>
                    <button class="btn-danger" onclick="sfDemoUI.provideIntervention('abort')">Abort</button>
                </div>
            </div>
        `;
    }

    // Provide intervention response
    provideIntervention(action) {
        const container = document.getElementById('sf-intervention-panel');
        if (container) container.style.display = 'none';

        this.executor.provideInterventionResponse({ action });
    }
}

// Global instance
let sfDemoUI = null;

function initStepFunctionDemo() {
    sfDemoUI = new StepFunctionDemoUI();
    sfDemoUI.init();
}
