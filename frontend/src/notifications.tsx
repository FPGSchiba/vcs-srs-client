import React from "react";
import ReactDOM from "react-dom/client";
import "./shared/styles/global.css";
import { NotificationsApp } from "./windows/notifications/NotificationsApp";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <NotificationsApp />
  </React.StrictMode>,
);
