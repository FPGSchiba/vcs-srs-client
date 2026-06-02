import React from "react";
import ReactDOM from "react-dom/client";
import "./shared/styles/global.css";
import { CommsApp } from "./windows/comms/CommsApp";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <CommsApp />
  </React.StrictMode>,
);
