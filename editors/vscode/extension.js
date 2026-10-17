// Starts the runtree language server (runtree-lsp) for the YAML files. The server decides which files are runtree
// files: the files that have a top level "tree" object, the files that they include, and *.rt.yaml files.
const vscode = require('vscode');
const { LanguageClient } = require('vscode-languageclient/node');

let client;

async function start() {
  const config = vscode.workspace.getConfiguration('runtree.languageServer');
  if (!config.get('enabled', true)) {
    return;
  }
  const command = config.get('path') || 'runtree-lsp';
  client = new LanguageClient('runtree', 'runtree', { command, args: [] }, {
    documentSelector: [{ scheme: 'file', language: 'yaml' }],
    synchronize: {
      // the trees of the workspace and the included files can change outside the editor
      fileEvents: vscode.workspace.createFileSystemWatcher('**/*.{yaml,yml}'),
    },
  });
  try {
    await client.start();
  } catch (err) {
    client = undefined;
    vscode.window.showWarningMessage(`runtree: the language server (${command}) could not be started: ` +
      `${err.message}. Set runtree.languageServer.path, or disable it with runtree.languageServer.enabled.`);
  }
}

async function stop() {
  if (client) {
    const c = client;
    client = undefined;
    await c.stop();
  }
}

function activate(context) {
  context.subscriptions.push(vscode.workspace.onDidChangeConfiguration(async (e) => {
    if (e.affectsConfiguration('runtree.languageServer')) {
      await stop();
      await start();
    }
  }));
  context.subscriptions.push(vscode.commands.registerCommand('runtree.restartLanguageServer', async () => {
    await stop();
    await start();
  }));
  return start();
}

function deactivate() {
  return stop();
}

module.exports = { activate, deactivate };
