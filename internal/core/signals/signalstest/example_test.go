package signalstest_test

import (
	"context"
	"fmt"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/core/signals/signalstest"
	"github.com/zedgama3/bms/internal/model"
)

func ExampleFake() {
	const greeted = signals.Name("hello.greeted")
	bus, err := signalstest.NewFake("hello", []signals.Name{greeted}, []signals.Name{greeted})
	if err != nil {
		fmt.Println(err)
		return
	}
	err = bus.Subscribe(greeted, func(ctx context.Context, s signals.Signal) error {
		_, err := signals.Get[model.PersonID](s.Payload, "person")
		fmt.Println("handler ran in delivery:", signals.InDelivery(ctx), err)
		return err
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	ctx := context.Background()
	database := dbtest.NewFake()
	err = database.Transact(ctx, db.Write, func(ctx context.Context) error {
		return bus.Publish(ctx, greeted, signals.NewPayload(signals.Ref("person", model.NewID[model.PersonID]())))
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	published := bus.Published()
	fmt.Println("published:", len(published), bus.Handlers())
	fmt.Println(bus.Deliver(ctx, published[0]))
	// Output:
	// published: 1 [hello.greeted]
	// handler ran in delivery: true <nil>
	// <nil>
}
