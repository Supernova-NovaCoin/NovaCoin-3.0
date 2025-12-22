import { useState, useEffect } from 'react'
import * as ed from '@noble/ed25519'
import './App.css'

function App() {
  const [privKey, setPrivKey] = useState('')
  const [pubKey, setPubKey] = useState('')
  const [logs, setLogs] = useState<string[]>([])
  const [ws, setWs] = useState<WebSocket | null>(null)

  useEffect(() => {
    // Connect to Gateway
    const socket = new WebSocket('ws://localhost:3000/ws')
    socket.onopen = () => addLog('✅ Connected to Supernova Gateway')
    socket.onmessage = (e) => addLog(`Gateway: ${e.data}`)
    setWs(socket)
    return () => socket.close()
  }, [])

  const addLog = (msg: string) => setLogs(prev => [msg, ...prev])

  const generateWallet = async () => {
    const priv = ed.utils.randomPrivateKey()
    const pub = await ed.getPublicKeyAsync(priv)
    setPrivKey(ed.utils.bytesToHex(priv))
    setPubKey(ed.utils.bytesToHex(pub))
    addLog(`🔑 Generated Wallet: ${ed.utils.bytesToHex(pub).slice(0, 10)}...`)
  }

  const sendTx = () => {
    if (!ws || !privKey) return
    // Mock Transaction Payload
    const payload = `TX:${pubKey}:MOCK_DEST:100`
    ws.send(payload)
    addLog(`🚀 Sent Transaction: ${payload}`)
  }

  return (
    <div className="container">
      <h1>⚡ Supernova Web Wallet</h1>
      
      <div className="card">
        <button onClick={generateWallet}>Create Quantum Wallet</button>
        {pubKey && (
          <div className="keys">
            <p><strong>Public:</strong> {pubKey.slice(0, 20)}...</p>
            <p><strong>Private:</strong> •••••••••••••••••</p>
          </div>
        )}
      </div>

      <div className="card">
        <button onClick={sendTx} disabled={!pubKey}>Send 100 NVN (Test)</button>
      </div>

      <div className="logs">
        <h3>Network Activity</h3>
        {logs.map((l, i) => <div key={i}>{l}</div>)}
      </div>
    </div>
  )
}

export default App
