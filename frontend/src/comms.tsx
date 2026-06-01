import React from "react";
import ReactDOM from "react-dom/client";
import "./shared/styles/global.css";

function Comms() {
  return (
    <div className="h-full grid place-items-center text-tx-0">
      <div className="font-mono text-ac-primary tracking-widest">
        VCS · comms popout · placeholder
      </div>
    </div>
  );
}

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <Comms />
  </React.StrictMode>,
);
