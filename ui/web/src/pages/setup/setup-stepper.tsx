import { Check } from "lucide-react";
import { useTranslation } from "react-i18next";

interface SetupStepperProps {
  currentStep: 1 | 2 | 3;
  completedSteps: number[];
}

export function SetupStepper({ currentStep, completedSteps }: SetupStepperProps) {
  const { t } = useTranslation("setup");

  const STEPS = [
    { num: 1, label: t("steps.provider") },
    { num: 2, label: t("steps.model") },
    { num: 3, label: t("steps.agent") },
  ];

  return (
    <div className="flex items-center justify-center">
      {STEPS.map((step, i) => {
        const completed = completedSteps.includes(step.num);
        const current = step.num === currentStep;
        return (
          <div key={step.num} className="flex items-center">
            <div className="flex flex-col items-center gap-1.5">
              <div
                className={
                  "flex h-9 w-9 items-center justify-center rounded-full text-sm font-medium transition-colors " +
                  (completed
                    ? "bg-primary text-primary-foreground"
                    : current
                      ? "border-2 border-primary bg-background text-primary"
                      : "border border-muted-foreground/30 bg-muted text-muted-foreground")
                }
              >
                {completed ? <Check className="h-4 w-4" /> : step.num}
              </div>
              <span
                className={
                  "text-xs font-medium " +
                  (current || completed ? "text-foreground" : "text-muted-foreground")
                }
              >
                {step.label}
              </span>
            </div>
            {i < STEPS.length - 1 && (
              <div
                className={
                  "mx-3 mb-6 h-0.5 w-12 sm:w-20 " +
                  (completed ? "bg-primary" : "bg-muted-foreground/20")
                }
              />
            )}
          </div>
        );
      })}
    </div>
  );
}
