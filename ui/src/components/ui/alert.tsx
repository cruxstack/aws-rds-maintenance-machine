import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "@/lib/utils"

const alertVariants = cva(
  "relative w-full rounded-md border px-3 py-2 text-xs grid has-[>svg]:grid-cols-[auto_1fr] has-[>svg]:gap-x-2 gap-y-0.5 items-start [&>svg]:size-3.5 [&>svg]:translate-y-px [&>svg]:text-current",
  {
    variants: {
      variant: {
        default: "bg-card text-foreground",
        info: "border-status-blue/30 bg-status-blue/10 text-foreground [&>svg]:text-status-blue",
        warning: "border-status-yellow/30 bg-status-yellow/10 text-foreground [&>svg]:text-status-yellow",
        destructive: "border-status-red/30 bg-status-red/10 text-foreground [&>svg]:text-status-red",
        success: "border-status-green/30 bg-status-green/10 text-foreground [&>svg]:text-status-green",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
)

function Alert({
  className,
  variant,
  ...props
}: React.ComponentProps<"div"> & VariantProps<typeof alertVariants>) {
  return (
    <div
      data-slot="alert"
      role="alert"
      className={cn(alertVariants({ variant }), className)}
      {...props}
    />
  )
}

function AlertTitle({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="alert-title"
      className={cn("col-start-2 font-medium leading-tight tracking-tight", className)}
      {...props}
    />
  )
}

function AlertDescription({
  className,
  ...props
}: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="alert-description"
      className={cn("col-start-2 text-xs text-muted-foreground [&_p]:leading-relaxed", className)}
      {...props}
    />
  )
}

export { Alert, AlertTitle, AlertDescription }
