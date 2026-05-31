import { useState } from 'react';
import './App.css';
import { Greet, OpenNewWindow } from '../bindings/myproject/app/app';

function App() {
    const [resultText, setResultText] = useState('Please enter your name below 👇');
    const [name, setName] = useState('');

    const updateName = (e: React.ChangeEvent<HTMLInputElement>) => setName(e.target.value);

    function greet() {
        Greet(name).then(setResultText);
    }

    function openWindow() {
        OpenNewWindow('Second Window');
    }

    return (
        <div id="App">
            <div id="result" className="result">{resultText}</div>
            <div id="input" className="input-box">
                <input
                    id="name"
                    className="input"
                    onChange={updateName}
                    autoComplete="off"
                    name="input"
                    type="text"
                />
                <button className="btn" onClick={greet}>Greet</button>
                <button className="btn" onClick={openWindow}>Open Window</button>
            </div>
        </div>
    );
}

export default App;
