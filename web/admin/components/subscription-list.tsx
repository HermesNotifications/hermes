"use client";

import { useState, useTransition } from "react";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { PlusIcon, PencilIcon, Trash2Icon, CheckIcon, XIcon } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { subscriptionSchema, type SubscriptionFormData } from "@/lib/schemas/category";
import {
  createSubscription,
  updateSubscription,
  deleteSubscription,
} from "@/lib/actions/subscriptions";
import type { Subscription } from "@hermes-notifications/server";

interface SubscriptionListProps {
  categoryId: string;
  subscriptions: Subscription[];
}

function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9\s-]/g, "")
    .replace(/\s+/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-|-$/g, "");
}

interface AddRowProps {
  categoryId: string;
  onDone: () => void;
}

function AddRow({ categoryId, onDone }: AddRowProps) {
  const router = useRouter();
  const [isPending, startTransition] = useTransition();

  const {
    register,
    handleSubmit,
    watch,
    setValue,
    formState: { errors },
  } = useForm<SubscriptionFormData>({
    resolver: zodResolver(subscriptionSchema),
    defaultValues: { name: "", slug: "", sortOrder: 0 },
  });

  function handleNameChange(e: React.ChangeEvent<HTMLInputElement>) {
    const name = e.target.value;
    setValue("name", name);
    setValue("slug", slugify(name));
  }

  function onSubmit(data: SubscriptionFormData) {
    startTransition(async () => {
      try {
        await createSubscription(categoryId, {
          slug: data.slug,
          name: data.name,
          sortOrder: data.sortOrder,
        });
        toast.success("Subscription created");
        router.refresh();
        onDone();
      } catch (err) {
        const message = err instanceof Error ? err.message : "Failed to create subscription";
        toast.error(message);
      }
    });
  }

  const slugValue = watch("slug");

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="border rounded-lg p-4 bg-muted/30 space-y-3">
      <p className="text-sm font-medium">New Subscription</p>
      <div className="grid grid-cols-[1fr_1fr_6rem_auto] gap-3 items-start">
        <div className="space-y-1">
          <Label className="text-xs">Name</Label>
          <Input
            placeholder="Weekly Digest"
            {...register("name")}
            onChange={handleNameChange}
          />
          {errors.name && (
            <p className="text-xs text-destructive">{errors.name.message}</p>
          )}
        </div>
        <div className="space-y-1">
          <Label className="text-xs">Slug</Label>
          <Input
            placeholder="weekly-digest"
            value={slugValue}
            {...register("slug")}
          />
          {errors.slug && (
            <p className="text-xs text-destructive">{errors.slug.message}</p>
          )}
        </div>
        <div className="space-y-1">
          <Label className="text-xs">Sort Order</Label>
          <Input
            type="number"
            min={0}
            {...register("sortOrder", { valueAsNumber: true })}
          />
          {errors.sortOrder && (
            <p className="text-xs text-destructive">{errors.sortOrder.message}</p>
          )}
        </div>
        <div className="flex gap-1 pt-5">
          <Button type="submit" size="icon" variant="default" disabled={isPending}>
            <CheckIcon className="size-4" />
          </Button>
          <Button type="button" size="icon" variant="ghost" onClick={onDone} disabled={isPending}>
            <XIcon className="size-4" />
          </Button>
        </div>
      </div>
    </form>
  );
}

interface EditRowProps {
  subscription: Subscription;
  categoryId: string;
  onDone: () => void;
}

function EditRow({ subscription, categoryId, onDone }: EditRowProps) {
  const router = useRouter();
  const [isPending, startTransition] = useTransition();

  const {
    register,
    handleSubmit,
    formState: { errors },
  } = useForm<SubscriptionFormData>({
    resolver: zodResolver(subscriptionSchema),
    defaultValues: {
      name: subscription.name,
      slug: subscription.slug,
      sortOrder: subscription.sort_order,
    },
  });

  function onSubmit(data: SubscriptionFormData) {
    startTransition(async () => {
      try {
        await updateSubscription(subscription.id, categoryId, {
          name: data.name,
          sortOrder: data.sortOrder,
        });
        toast.success("Subscription updated");
        router.refresh();
        onDone();
      } catch (err) {
        const message = err instanceof Error ? err.message : "Failed to update subscription";
        toast.error(message);
      }
    });
  }

  return (
    <form onSubmit={handleSubmit(onSubmit)}>
      <div className="grid grid-cols-[1fr_1fr_6rem_auto] gap-3 items-start py-2">
        <div className="space-y-1">
          <Input
            placeholder="Name"
            {...register("name")}
          />
          {errors.name && (
            <p className="text-xs text-destructive">{errors.name.message}</p>
          )}
        </div>
        <div>
          <Input
            value={subscription.slug}
            disabled
            className="text-muted-foreground"
          />
        </div>
        <div className="space-y-1">
          <Input
            type="number"
            min={0}
            {...register("sortOrder", { valueAsNumber: true })}
          />
          {errors.sortOrder && (
            <p className="text-xs text-destructive">{errors.sortOrder.message}</p>
          )}
        </div>
        <div className="flex gap-1">
          <Button type="submit" size="icon" variant="default" disabled={isPending}>
            <CheckIcon className="size-4" />
          </Button>
          <Button type="button" size="icon" variant="ghost" onClick={onDone} disabled={isPending}>
            <XIcon className="size-4" />
          </Button>
        </div>
      </div>
    </form>
  );
}

interface SubscriptionRowProps {
  subscription: Subscription;
  categoryId: string;
}

function SubscriptionRow({ subscription, categoryId }: SubscriptionRowProps) {
  const router = useRouter();
  const [isEditing, setIsEditing] = useState(false);
  const [isPending, startTransition] = useTransition();

  function handleDelete() {
    startTransition(async () => {
      try {
        await deleteSubscription(subscription.id, categoryId);
        toast.success("Subscription deleted");
        router.refresh();
      } catch (err) {
        const message = err instanceof Error ? err.message : "Failed to delete subscription";
        toast.error(message);
      }
    });
  }

  if (isEditing) {
    return (
      <EditRow
        subscription={subscription}
        categoryId={categoryId}
        onDone={() => setIsEditing(false)}
      />
    );
  }

  return (
    <div className="grid grid-cols-[1fr_1fr_6rem_auto] gap-3 items-center py-2 border-b last:border-0">
      <span className="text-sm font-medium">{subscription.name}</span>
      <code className="rounded bg-muted px-1.5 py-0.5 text-xs font-mono w-fit">
        {subscription.slug}
      </code>
      <span className="text-sm text-muted-foreground">{subscription.sort_order}</span>
      <div className="flex gap-1">
        <Button
          type="button"
          size="icon"
          variant="ghost"
          onClick={() => setIsEditing(true)}
          disabled={isPending}
        >
          <PencilIcon className="size-4" />
          <span className="sr-only">Edit</span>
        </Button>
        <ConfirmDialog
          trigger={
            <Button
              type="button"
              size="icon"
              variant="ghost"
              className="text-destructive hover:text-destructive"
              disabled={isPending}
            >
              <Trash2Icon className="size-4" />
              <span className="sr-only">Delete</span>
            </Button>
          }
          title="Delete Subscription"
          description={`Are you sure you want to delete "${subscription.name}"? This action cannot be undone.`}
          onConfirm={handleDelete}
          destructive
        />
      </div>
    </div>
  );
}

export function SubscriptionList({ categoryId, subscriptions }: SubscriptionListProps) {
  const [isAdding, setIsAdding] = useState(false);

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold">Subscriptions</h2>
          <p className="text-sm text-muted-foreground">
            Manage subscription types within this category.
          </p>
        </div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => setIsAdding(true)}
          disabled={isAdding}
        >
          <PlusIcon className="size-4" />
          Add Subscription
        </Button>
      </div>

      {subscriptions.length > 0 && (
        <div className="rounded-lg border px-4">
          <div className="grid grid-cols-[1fr_1fr_6rem_auto] gap-3 py-2 border-b">
            <span className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Name</span>
            <span className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Slug</span>
            <span className="text-xs font-medium text-muted-foreground uppercase tracking-wide">Order</span>
            <span />
          </div>
          {subscriptions.map((sub) => (
            <SubscriptionRow key={sub.id} subscription={sub} categoryId={categoryId} />
          ))}
        </div>
      )}

      {subscriptions.length === 0 && !isAdding && (
        <div className="rounded-lg border border-dashed px-6 py-8 text-center">
          <p className="text-sm text-muted-foreground">No subscriptions yet.</p>
          <p className="text-xs text-muted-foreground mt-1">
            Add subscriptions to let users control their notification preferences.
          </p>
        </div>
      )}

      {isAdding && (
        <AddRow categoryId={categoryId} onDone={() => setIsAdding(false)} />
      )}
    </div>
  );
}
