import { Search } from "lucide-react";
import { Input } from "@/components/ui/input";

type SearchFieldProps = React.ComponentProps<typeof Input>;

export function SearchField({ className, ...props }: SearchFieldProps) {
  return (
    <div className={className}>
      <div className="relative">
        <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input className="pl-9" {...props} />
      </div>
    </div>
  );
}
