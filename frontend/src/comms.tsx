import React from "react";
import ReactDOM from "react-dom/client";
import "./shared/styles/global.css";
import { Placeholder } from "./shared/components/Placeholder";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <Placeholder label="VCS · comms popout · placeholder" />
  </React.StrictMode>,
);
