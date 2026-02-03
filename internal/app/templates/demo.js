// Demo mode JavaScript for RDS Maintenance Machine
// This file contains demo-specific functionality (mock server controls, fault injection, etc.)
// Note: Cluster selection and status now use the same API as production mode (in main.js)

const mockEndpoint = 'http://localhost:9080';

// Track if Step Functions demo has been initialized
let sfDemoInitialized = false;

// Tab switching
function switchTab(tab) {
    document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
    document.querySelectorAll('.tab-content').forEach(t => t.classList.remove('active'));
    document.querySelector('.tab[onclick*="' + tab + '"]').classList.add('active');
    document.getElementById('tab-' + tab).classList.add('active');
    
    if (tab === 'demo') {
        loadMockState();
    } else if (tab === 'stepfn') {
        // Initialize Step Functions demo on first visit
        if (!sfDemoInitialized && typeof initStepFunctionDemo === 'function') {
            initStepFunctionDemo();
            sfDemoInitialized = true;
        }
    }
}

// Update slider value display on input
function updateSliderValue(id) {
    const slider = document.getElementById(id);
    const valueDisplay = document.getElementById(id + '-value');
    valueDisplay.textContent = slider.value + 'ms';
}

// Fault parameter templates
const faultParamTemplates = {
    api_error: '<div class="form-group"><label>Error Code</label><input type="text" id="fault-error-code" value="InternalFailure"></div><div class="form-group"><label>Error Message</label><input type="text" id="fault-error-msg" value="Injected fault"></div>',
    delay: '<div class="form-group"><label>Delay (ms)</label><input type="number" id="fault-delay" value="5000"></div>',
    stuck: ''
};

// Load mock server state (for Demo Controls tab)
async function loadMockState() {
    try {
        const response = await fetch(mockEndpoint + '/mock/state');
        const state = await response.json();
        renderDemoControlsClusters(state.clusters, state.instances);
        renderFaults(state.faults);
        
        // Update timing controls
        document.getElementById('fast-mode').checked = state.timing.FastMode;
        document.getElementById('base-wait').value = state.timing.BaseWaitMs;
        document.getElementById('base-wait-value').textContent = state.timing.BaseWaitMs + 'ms';
        document.getElementById('random-range').value = state.timing.RandomRangeMs;
        document.getElementById('random-range-value').textContent = state.timing.RandomRangeMs + 'ms';
    } catch (err) {
        console.error('failed to load mock state:', err);
    }
}

// Render clusters in Demo Controls tab (shows raw mock state for debugging)
function renderDemoControlsClusters(clusters, instances) {
    const container = document.getElementById('clusters-container');
    if (!container) return;
    
    if (!clusters || clusters.length === 0) {
        container.innerHTML = '<div class="empty-state" style="padding: 24px;">No clusters</div>';
        return;
    }
    
    container.innerHTML = clusters.map(cluster => {
        const clusterInstances = instances.filter(i => i.ClusterID === cluster.ID);
        return '<div class="cluster-card">' +
            '<div class="cluster-header">' +
                '<span class="cluster-name">' + cluster.ID + '</span>' +
                '<span class="state-badge state-' + cluster.Status + '">' + cluster.Status + '</span>' +
            '</div>' +
            '<div class="cluster-info">' + cluster.Engine + ' ' + cluster.EngineVersion + '</div>' +
            '<table class="data-table cols-5">' +
                '<thead>' +
                    '<tr>' +
                        '<th>Instance</th>' +
                        '<th>Role</th>' +
                        '<th>Type</th>' +
                        '<th>PI</th>' +
                        '<th>Status</th>' +
                    '</tr>' +
                '</thead>' +
                '<tbody>' +
                    clusterInstances.map(inst => {
                        let roleClass = inst.IsWriter ? 'writer' : (inst.IsAutoScaled ? 'autoscaled' : '');
                        let roleLabel = inst.IsWriter ? 'W' : (inst.IsAutoScaled ? 'A' : 'R');
                        let piStatus = '-';
                        if (inst.PerformanceInsightsEnabled) {
                            piStatus = '<span class="pi-badge enabled">ON</span>';
                        } else if (inst.Status === 'configuring-performance-insights') {
                            piStatus = '<span class="pi-badge enabling">...</span>';
                        }
                        return '<tr>' +
                            '<td style="font-weight: 500;">' + inst.ID + '</td>' +
                            '<td><span class="instance-role ' + roleClass + '">' + roleLabel + '</span></td>' +
                            '<td style="color: var(--muted-foreground);">' + inst.InstanceType + '</td>' +
                            '<td>' + piStatus + '</td>' +
                            '<td>' + formatStatus(inst.Status) + '</td>' +
                        '</tr>';
                    }).join('') +
                '</tbody>' +
            '</table>' +
        '</div>';
    }).join('');
}

// Render faults list
function renderFaults(faults) {
    const container = document.getElementById('faults-list');
    if (!container) return;
    
    if (!faults || faults.length === 0) {
        container.innerHTML = '<div class="empty-state" style="padding: 20px;">No faults configured</div>';
        return;
    }
    
    const typeNames = {
        api_error: 'API Error',
        delay: 'Extra Delay',
        stuck: 'Stuck in State',
        partial_fail: 'Partial Fail'
    };
    
    container.innerHTML = faults.map(fault => {
        return '<div class="fault-item">' +
            '<div class="fault-info">' +
                '<div class="fault-type">' + typeNames[fault.type] + ' (' + Math.round(fault.probability * 100) + '%)</div>' +
                '<div class="fault-target">' + (fault.action || 'Any action') + (fault.target ? ' on ' + fault.target : '') + '</div>' +
            '</div>' +
            '<div class="fault-actions">' +
                '<label class="toggle">' +
                    '<input type="checkbox" ' + (fault.enabled ? 'checked' : '') + ' onchange="toggleFault(\'' + fault.id + '\', this.checked)">' +
                    '<span class="toggle-slider"></span>' +
                '</label>' +
                '<button onclick="deleteFault(\'' + fault.id + '\')" class="btn-xs btn-danger">X</button>' +
            '</div>' +
        '</div>';
    }).join('');
}

// Update timing settings
async function updateTiming(fromSlider = false) {
    const fastModeCheckbox = document.getElementById('fast-mode');
    
    // If a slider was adjusted and fast mode is on, turn it off
    // (adjusting sliders implies you want custom timing, not fast mode)
    if (fromSlider && fastModeCheckbox.checked) {
        fastModeCheckbox.checked = false;
    }
    
    const timing = {
        BaseWaitMs: parseInt(document.getElementById('base-wait').value),
        RandomRangeMs: parseInt(document.getElementById('random-range').value),
        FastMode: fastModeCheckbox.checked
    };
    
    document.getElementById('base-wait-value').textContent = timing.BaseWaitMs + 'ms';
    document.getElementById('random-range-value').textContent = timing.RandomRangeMs + 'ms';
    
    try {
        await fetch(mockEndpoint + '/mock/timing', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(timing)
        });
    } catch (err) {
        console.error('failed to update timing:', err);
    }
}

// Reset all mock state
async function resetState() {
    const confirmed = await showConfirmDialog(
        'Reset all mock state and clear operations? This cannot be undone.',
        'Reset State',
        'Reset'
    );
    if (!confirmed) return;
    
    try {
        // Reset mock RDS state
        await fetch(mockEndpoint + '/mock/reset', { method: 'POST' });
        // Delete all operations
        await fetch('/api/operations', { method: 'DELETE' });
        // Clear selected operation
        selectedOperationId = null;
        if (pollInterval) {
            clearInterval(pollInterval);
            pollInterval = null;
        }
        document.getElementById('operation-detail').innerHTML = 
            '<div class="empty-state"><p>Select an operation to view details</p></div>';
        // Reload UI - regions/clusters will reload via API
        loadMockState();
        loadOperations();
        loadRegions();
    } catch (err) {
        showAlertDialog(err.message);
    }
}

// Toggle fault enabled state
async function toggleFault(id, enabled) {
    try {
        await fetch(mockEndpoint + '/mock/faults/' + id, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ enabled })
        });
    } catch (err) {
        console.error('failed to toggle fault:', err);
    }
}

// Delete a fault
async function deleteFault(id) {
    try {
        await fetch(mockEndpoint + '/mock/faults/' + id, { method: 'DELETE' });
        loadMockState();
    } catch (err) {
        console.error('failed to delete fault:', err);
    }
}

// Clear all faults
async function clearFaults() {
    const confirmed = await showConfirmDialog(
        'Clear all fault injection rules?',
        'Clear Faults',
        'Clear'
    );
    if (!confirmed) return;
    
    try {
        await fetch(mockEndpoint + '/mock/faults', { method: 'DELETE' });
        loadMockState();
    } catch (err) {
        showAlertDialog(err.message);
    }
}

// Setup fault form
function setupFaultForm() {
    const faultType = document.getElementById('fault-type');
    const faultParams = document.getElementById('fault-params');
    const faultForm = document.getElementById('fault-form');
    
    if (!faultType || !faultParams || !faultForm) return;
    
    // Initialize fault params
    faultParams.innerHTML = faultParamTemplates['api_error'];
    
    faultType.addEventListener('change', function() {
        faultParams.innerHTML = faultParamTemplates[this.value] || '';
    });
    
    faultForm.addEventListener('submit', async function(e) {
        e.preventDefault();
        const faultTypeValue = faultType.value;
        const fault = {
            type: faultTypeValue,
            action: document.getElementById('fault-action').value,
            target: document.getElementById('fault-target').value,
            probability: parseInt(document.getElementById('fault-probability').value) / 100,
            enabled: true
        };
        
        if (faultTypeValue === 'api_error') {
            fault.error_code = document.getElementById('fault-error-code').value;
            fault.error_message = document.getElementById('fault-error-msg').value;
        } else if (faultTypeValue === 'delay') {
            fault.delay_ms = parseInt(document.getElementById('fault-delay').value);
        }
        
        try {
            await fetch(mockEndpoint + '/mock/faults', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(fault)
            });
            loadMockState();
        } catch (err) {
            showAlertDialog(err.message);
        }
    });
}

// Initialize demo mode (called from main.js initApp)
function initDemoMode() {
    setupFaultForm();
    // Note: Cluster selection now handled by main.js (loadRegions, loadClusters)
    // The Demo Controls tab shows raw mock state for debugging purposes
}
