# Pitch Chat semantic parity

This example independently reproduces the observable behavior shared by the
Pitch pRTI IEEE 1516-2010 `chat-cpp` and `chat-java` samples. It does not copy
or compile Pitch source code.

The two gorti federates exercise federation lifecycle, publication and
subscription by HLA handle, object-instance name reservation,
`Participant.Name` discovery and reflection, `Communication.Message` and
`Communication.Sender` delivery, and object removal caused by resign-with-delete.

Run `rtid` first and then execute:

```powershell
python examples/pitch-chat-parity/chat_scenario.py grpc://127.0.0.1:8080
```

The machine-readable contract is
`tests/pitch_examples/contracts/chat-1516e.json`. The integration test checks
required events, payloads, object identity, and only required happens-before
relations. It does not require one total callback order because language and
thread scheduling can differ without changing HLA semantics.

The installed Pitch package also contains IEEE 1516-2000, DLC, and HLA 1.3
Chat variants. Those APIs are outside gorti's IEEE 1516-2010 scope and are
listed explicitly as unsupported in `tests/pitch_examples/pitch_examples.json`.
