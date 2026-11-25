import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./index.css";
import StyrixaFactoryConfigurator from "./StyrixaFactoryConfigurator.jsx";

createRoot(document.getElementById("root")).render(
  <StrictMode>
    <StyrixaFactoryConfigurator />
  </StrictMode>
);
