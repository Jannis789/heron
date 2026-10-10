| Prinzip | Bei uns |
|---|---|
| Model = kompletter Zustand | Model{input, chat, send, theme, copyFn} — alles Zustand, nichts globales |
| Update(Msg) → (Model, Cmd) | genau die Bubble-Tea-Signatur; Senden ist ein tea.Cmd (blockiert die UI nie), die Antwort kommt als replyMsg zurück in Update — der klassische Msg-Kreislauf |
| View = pur | View() ist eine reine String-Funktion des Zustands, keine Seiteneffekte |
| Cmd statt Seiteneffekt (netzwerkfähig) | send(prompt) läuft ausschließlich im Cmd |
| Nested Components | Jeder Block ist selbst eine Mini-Elm-Architektur (Widget mit Update/View), BlockList routet Msgs an das fokussierte Kind — genau das Komponenten-Muster, das bubbles vorlebt (textinput machen wir 1:1 genauso) |
