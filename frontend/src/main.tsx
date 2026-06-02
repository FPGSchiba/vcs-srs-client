import React from "react";
import ReactDOM from "react-dom/client";
import "./shared/styles/global.css";
import { MainApp } from "./windows/main/MainApp";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <MainApp />
  </React.StrictMode>,
);
